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
	"github.com/n0remac/Knowledge-Graph/internal/generate"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/store"
)

const maxDiscordReplyRunes = 2000

type Runtime struct {
	cfg       config.Config
	store     *store.Store
	extractor *memory.Extractor
	retriever *memory.Retriever
	generator *generate.Generator
	session   *discordgo.Session
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	db, err := store.NewGraph(cfg.GraphStorePath)
	if err != nil {
		return nil, err
	}

	llmClient := ollama.NewClient(cfg.OllamaBaseURL, cfg.RequestTimeout)
	extractor := memory.NewExtractor(llmClient, cfg.OllamaExtractModel)
	retriever := memory.NewRetriever(db, cfg.RecentMessageLimit, cfg.RecallFactLimit, cfg.RecallTopicLimit)
	generator := generate.NewGenerator(llmClient, cfg.OllamaChatModel, cfg.Persona)

	session, err := discordgo.New("Bot " + cfg.DiscordBotToken)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	runtime := &Runtime{
		cfg:       cfg,
		store:     db,
		extractor: extractor,
		retriever: retriever,
		generator: generator,
		session:   session,
	}

	session.AddHandler(runtime.onReady)
	session.AddHandler(runtime.onMessageCreate)
	return runtime, nil
}

func (r *Runtime) Run() error {
	log.Printf(
		"startup mode=%q graph_store_path=%q ollama_base_url=%q chat_model=%q extract_model=%q",
		"graph-recall",
		r.cfg.GraphStorePath,
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

	log.Printf("discord connection established; graph memory loop active")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Printf("shutdown signal received")
	return nil
}

func (r *Runtime) Close() error {
	if r.store != nil {
		return r.store.Close()
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

	message := models.Message{
		ID:        event.ID,
		ChannelID: event.ChannelID,
		GuildID:   event.GuildID,
		AuthorID:  event.Author.ID,
		Author:    event.Author.Username,
		Content:   content,
		Timestamp: timestamp,
		ReplyToID: replyToID,
	}

	log.Printf(
		"event=message message_id=%q guild_id=%q channel_id=%q author_id=%q content=%q",
		message.ID, message.GuildID, message.ChannelID, message.AuthorID, message.Content,
	)

	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.RequestTimeout)
	defer cancel()

	if err := r.store.UpsertUser(ctx, message.AuthorID, event.Author.Username, event.Author.GlobalName, message.Timestamp); err != nil {
		log.Printf("event=store_user_error message_id=%q err=%q", message.ID, err.Error())
	}
	if err := r.store.SaveMessage(ctx, message); err != nil {
		log.Printf("event=store_message_error message_id=%q err=%q", message.ID, err.Error())
	}

	extraction, err := r.extractor.ExtractFromMessage(ctx, message)
	if err != nil {
		log.Printf("event=extract_error message_id=%q err=%q", message.ID, err.Error())
	} else {
		r.persistExtraction(ctx, message, extraction)
	}
	if err := r.store.Flush(); err != nil {
		log.Printf("event=store_flush_error message_id=%q err=%q", message.ID, err.Error())
	}

	retrieveInput := memory.RetrieveInput{
		ChannelID:       message.ChannelID,
		SpeakerID:       message.AuthorID,
		MentionedUserID: extractMentionIDs(event),
	}
	bundle, err := r.retriever.Retrieve(ctx, retrieveInput)
	if err != nil {
		log.Printf("event=retrieve_error message_id=%q err=%q", message.ID, err.Error())
		bundle = models.RetrievalBundle{}
	}

	reply, err := r.generator.GenerateReply(ctx, message, bundle)
	if err != nil {
		log.Printf("event=generate_error message_id=%q err=%q", message.ID, err.Error())
		_, sendErr := session.ChannelMessageSend(message.ChannelID, "I couldn't generate a response right now.")
		if sendErr != nil {
			log.Printf("event=send_error message_id=%q err=%q", message.ID, sendErr.Error())
		}
		return
	}

	reply = trimToDiscordLimit(strings.TrimSpace(reply))
	if reply == "" {
		return
	}
	if _, err := session.ChannelMessageSend(message.ChannelID, reply); err != nil {
		log.Printf("event=send_error message_id=%q err=%q", message.ID, err.Error())
		return
	}
	log.Printf("event=reply_sent source_message_id=%q reply_len=%d", message.ID, len([]rune(reply)))
}

func (r *Runtime) persistExtraction(ctx context.Context, sourceMessage models.Message, extraction memory.ExtractionResult) {
	for _, topicName := range extraction.Topics {
		topic, err := r.store.UpsertTopic(ctx, topicName, sourceMessage.Timestamp)
		if err != nil {
			log.Printf("event=topic_upsert_error message_id=%q topic=%q err=%q", sourceMessage.ID, topicName, err.Error())
			continue
		}
		if err := r.store.LinkMessageTopic(ctx, sourceMessage.ID, topic.ID); err != nil {
			log.Printf("event=topic_link_error message_id=%q topic_id=%d err=%q", sourceMessage.ID, topic.ID, err.Error())
		}
	}

	for _, fact := range extraction.Facts {
		if strings.TrimSpace(fact.SubjectID) == "" {
			fact.SubjectID = sourceMessage.AuthorID
		}
		if _, err := r.store.UpsertFactFromMessage(ctx, fact, sourceMessage.ID, r.cfg.OllamaExtractModel, sourceMessage.Timestamp); err != nil {
			log.Printf("event=fact_upsert_error message_id=%q kind=%q err=%q", sourceMessage.ID, fact.Kind, err.Error())
		}
	}
}

func extractMentionIDs(event *discordgo.MessageCreate) []string {
	seen := map[string]struct{}{}
	var ids []string
	for _, user := range event.Mentions {
		if user == nil || user.Bot {
			continue
		}
		if _, ok := seen[user.ID]; ok {
			continue
		}
		seen[user.ID] = struct{}{}
		ids = append(ids, user.ID)
	}
	return ids
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
