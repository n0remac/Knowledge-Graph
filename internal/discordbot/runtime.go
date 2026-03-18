package discordbot

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/n0remac/Knowledge-Graph/internal/config"
	"github.com/n0remac/Knowledge-Graph/internal/conversation"
	"github.com/n0remac/Knowledge-Graph/internal/generate"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

const (
	maxDiscordReplyRunes = 2000
	botTestingChannel    = "bot-testing"
)

type Runtime struct {
	cfg               config.Config
	conversationStore *conversation.Store
	engine            *conversation.Engine
	generator         *generate.Generator
	session           *discordgo.Session
	telemetry         *telemetry.Manager
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	if err := config.ValidateBotConfig(cfg); err != nil {
		return nil, err
	}

	session, err := discordgo.New("Bot " + cfg.DiscordBotToken)
	if err != nil {
		return nil, err
	}
	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	manager, err := telemetry.NewManager(cfg.Telemetry, session)
	if err != nil {
		return nil, err
	}

	conversationStore, err := conversation.NewStore(cfg.ConversationStorePath, manager)
	if err != nil {
		_ = manager.Close()
		return nil, err
	}

	llmClient := ollama.NewClient(cfg.OllamaBaseURL, cfg.RequestTimeout)
	engine := conversation.NewEngine(conversationStore, llmClient, cfg.OllamaExtractModel, cfg.RequestTimeout, manager)
	generator := generate.NewGenerator(llmClient, cfg.OllamaChatModel, cfg.Persona, manager)

	runtime := &Runtime{
		cfg:               cfg,
		conversationStore: conversationStore,
		engine:            engine,
		generator:         generator,
		session:           session,
		telemetry:         manager,
	}

	session.AddHandler(runtime.onReady)
	session.AddHandler(runtime.onMessageCreate)
	return runtime, nil
}

func (r *Runtime) Run() error {
	log.Printf(
		"startup mode=%q conversation_store_path=%q ollama_base_url=%q chat_model=%q extract_model=%q",
		"conversation-state",
		r.cfg.ConversationStorePath,
		r.cfg.OllamaBaseURL,
		r.cfg.OllamaChatModel,
		r.cfg.OllamaExtractModel,
	)

	if err := r.session.Open(); err != nil {
		return err
	}
	defer func() {
		_ = r.session.Close()
	}()

	log.Printf("discord connection established; live conversation state loop active")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Printf("shutdown signal received")
	return nil
}

func (r *Runtime) Close() error {
	if r.telemetry != nil {
		defer func() {
			if err := r.telemetry.Close(); err != nil {
				log.Printf("telemetry shutdown error: %v", err)
			}
		}()
	}
	if r.conversationStore != nil {
		if err := r.conversationStore.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) onReady(_ *discordgo.Session, ready *discordgo.Ready) {
	log.Printf(
		"event=ready user_id=%q username=%q guild_count=%d",
		ready.User.ID,
		ready.User.Username,
		len(ready.Guilds),
	)
}

func (r *Runtime) onMessageCreate(session *discordgo.Session, event *discordgo.MessageCreate) {
	if event.Author == nil || event.Author.Bot {
		return
	}
	if !isBotTestingChannel(session, event.ChannelID) {
		return
	}

	content := strings.TrimSpace(event.Content)
	if content == "" {
		return
	}

	timestamp := time.Now().UTC()
	if !event.Timestamp.IsZero() {
		timestamp = event.Timestamp
	}

	replyToID := ""
	if event.Message != nil && event.Message.Reference() != nil {
		replyToID = event.Message.Reference().MessageID
	}

	message := models.RawMessage{
		MessageID:        event.ID,
		ConversationID:   event.ChannelID,
		AuthorID:         event.Author.ID,
		AuthorRole:       "user",
		Content:          content,
		TimestampUnixMs:  timestamp.UnixMilli(),
		ReplyToMessageID: replyToID,
	}

	log.Printf(
		"event=message message_id=%q guild_id=%q channel_id=%q author_id=%q content=%q",
		message.MessageID, event.GuildID, message.ConversationID, message.AuthorID, message.Content,
	)

	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.RequestTimeout)
	defer cancel()
	ctx = telemetry.WithTraceID(ctx, message.MessageID)

	r.emit(ctx, telemetry.StageRuntime, "message_received", "discord message received", map[string]any{
		"message": messageTelemetryPayload(message, event.GuildID, event.Author.Username, event.Author.GlobalName),
	})

	var replyTarget *models.RawMessage
	if event.Message != nil && event.Message.ReferencedMessage != nil {
		referenced := event.Message.ReferencedMessage
		referencedTimestamp := time.Now().UTC().UnixMilli()
		if !referenced.Timestamp.IsZero() {
			referencedTimestamp = referenced.Timestamp.UnixMilli()
		}
		replyTarget = &models.RawMessage{
			MessageID:        referenced.ID,
			ConversationID:   referenced.ChannelID,
			AuthorID:         referenced.Author.ID,
			AuthorRole:       referencedAuthorRole(referenced.Author, session),
			Content:          strings.TrimSpace(referenced.Content),
			TimestampUnixMs:  referencedTimestamp,
			ReplyToMessageID: "",
		}
	}

	result, err := r.engine.ProcessMessage(ctx, message, conversation.ProcessOptions{ReplyTarget: replyTarget})
	if err != nil {
		log.Printf("event=conversation_process_error message_id=%q err=%q", message.MessageID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "conversation_process_error", "failed to update live conversation state", err, nil)
		_, sendErr := session.ChannelMessageSend(message.ConversationID, "I couldn't update my conversation state right now.")
		if sendErr != nil {
			log.Printf("event=send_error message_id=%q err=%q", message.MessageID, sendErr.Error())
			r.emitError(ctx, telemetry.StageRuntime, "send_error", "failed to send conversation fallback reply", sendErr, nil)
		}
		return
	}

	if err := session.ChannelTyping(message.ConversationID); err != nil {
		log.Printf("event=typing_error message_id=%q err=%q", message.MessageID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "typing_error", "failed to send typing indicator", err, nil)
	}

	reply, err := r.generator.GenerateReplyFromBrief(ctx, result.Message, result.ResponseContext.Brief)
	if err != nil {
		log.Printf("event=generate_error message_id=%q err=%q", message.MessageID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "generate_error", "reply generation failed", err, nil)
		_, sendErr := session.ChannelMessageSend(message.ConversationID, "I couldn't generate a response right now.")
		if sendErr != nil {
			log.Printf("event=send_error message_id=%q err=%q", message.MessageID, sendErr.Error())
			r.emitError(ctx, telemetry.StageRuntime, "send_error", "failed to send generation fallback reply", sendErr, nil)
		}
		return
	}
	r.emit(ctx, telemetry.StageRuntime, "reply_generated", "reply text generated", map[string]any{
		"reply":        reply,
		"reply_length": len([]rune(strings.TrimSpace(reply))),
	})

	reply = trimToDiscordLimit(strings.TrimSpace(reply))
	if reply == "" {
		r.emit(ctx, telemetry.StageRuntime, "reply_generated", "reply generation produced empty output", map[string]any{
			"reply_length": 0,
		})
		return
	}

	sentMessage, err := session.ChannelMessageSend(message.ConversationID, reply)
	if err != nil {
		log.Printf("event=send_error message_id=%q err=%q", message.MessageID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "send_error", "failed to send reply", err, map[string]any{
			"reply_length": len([]rune(reply)),
		})
		return
	}
	log.Printf("event=reply_sent source_message_id=%q reply_len=%d", message.MessageID, len([]rune(reply)))
	r.emit(ctx, telemetry.StageRuntime, "reply_sent", "reply sent to discord", map[string]any{
		"reply":        reply,
		"reply_length": len([]rune(reply)),
	})

	r.ingestAssistantReply(message, result.Message, sentMessage)
}

func (r *Runtime) ingestAssistantReply(source models.RawMessage, replyTarget models.RawMessage, sent *discordgo.Message) {
	if sent == nil {
		return
	}

	content := strings.TrimSpace(sent.Content)
	if content == "" {
		return
	}

	timestamp := time.Now().UTC()
	if !sent.Timestamp.IsZero() {
		timestamp = sent.Timestamp
	}

	authorID := "assistant"
	if sent.Author != nil && strings.TrimSpace(sent.Author.ID) != "" {
		authorID = sent.Author.ID
	} else if r.session != nil && r.session.State != nil && r.session.State.User != nil && strings.TrimSpace(r.session.State.User.ID) != "" {
		authorID = r.session.State.User.ID
	}

	assistantMessage := models.RawMessage{
		MessageID:        sent.ID,
		ConversationID:   source.ConversationID,
		AuthorID:         authorID,
		AuthorRole:       "assistant",
		Content:          content,
		TimestampUnixMs:  timestamp.UnixMilli(),
		ReplyToMessageID: source.MessageID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.RequestTimeout)
	defer cancel()
	ctx = telemetry.WithTraceID(ctx, assistantMessage.MessageID)

	r.emit(ctx, telemetry.StageRuntime, "assistant_message_ingested", "assistant reply queued for working state update", map[string]any{
		"message": messageTelemetryPayload(assistantMessage, sent.GuildID, assistantLabel(sent.Author), ""),
	})

	if _, err := r.engine.ProcessMessage(ctx, assistantMessage, conversation.ProcessOptions{ReplyTarget: &replyTarget}); err != nil {
		log.Printf("event=assistant_ingest_error message_id=%q err=%q", assistantMessage.MessageID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "assistant_ingest_error", "failed to process assistant reply into working state", err, nil)
	}
}

func referencedAuthorRole(author *discordgo.User, session *discordgo.Session) string {
	if author == nil {
		return "user"
	}
	if session != nil && session.State != nil && session.State.User != nil && author.ID == session.State.User.ID {
		return "assistant"
	}
	if author.Bot {
		return "assistant"
	}
	return "user"
}

func assistantLabel(author *discordgo.User) string {
	if author == nil {
		return "assistant"
	}
	if strings.TrimSpace(author.Username) != "" {
		return author.Username
	}
	return "assistant"
}

func messageTelemetryPayload(message models.RawMessage, guildID, authorUsername, authorDisplayName string) map[string]any {
	payload := map[string]any{
		"id":                  message.MessageID,
		"conversation_id":     message.ConversationID,
		"channel_id":          message.ConversationID,
		"guild_id":            guildID,
		"author_id":           message.AuthorID,
		"author_role":         message.AuthorRole,
		"author_username":     authorUsername,
		"author_display_name": authorDisplayName,
		"content":             message.Content,
		"reply_to_id":         message.ReplyToMessageID,
		"sequence_number":     message.SequenceNumber,
		"timestamp_unix_ms":   message.TimestampUnixMs,
	}
	if payload["author_username"] == "" {
		delete(payload, "author_username")
	}
	if payload["author_display_name"] == "" {
		delete(payload, "author_display_name")
	}
	return payload
}

func trimToDiscordLimit(input string) string {
	runes := []rune(input)
	if len(runes) <= maxDiscordReplyRunes {
		return input
	}
	cutoff := maxDiscordReplyRunes - 3
	if cutoff < 0 {
		cutoff = 0
	}
	return string(runes[:cutoff]) + "..."
}

func isBotTestingChannel(session *discordgo.Session, channelID string) bool {
	channel := getChannel(session, channelID)
	if channel == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(channel.Name), botTestingChannel)
}

func getChannel(session *discordgo.Session, channelID string) *discordgo.Channel {
	if session == nil || channelID == "" {
		return nil
	}
	if channel, err := session.State.Channel(channelID); err == nil {
		return channel
	}
	channel, err := session.Channel(channelID)
	if err != nil {
		return nil
	}
	return channel
}

func (r *Runtime) emit(ctx context.Context, stage, kind, summary string, payload map[string]any) {
	if r == nil || r.telemetry == nil {
		return
	}
	r.telemetry.Emit(ctx, stage, kind, summary, payload)
}

func (r *Runtime) emitError(ctx context.Context, stage, kind, summary string, err error, payload map[string]any) {
	if payload == nil {
		payload = make(map[string]any, 1)
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	r.emit(ctx, stage, kind, summary, payload)
}
