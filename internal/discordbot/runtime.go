package discordbot

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
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

const (
	maxDiscordReplyRunes = 2000
	maxTopicsPerMessage  = 2
)

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
	mentionedUserIDs := extractMentionIDs(event)

	timestamp := time.Now().UTC()
	if !event.Timestamp.IsZero() {
		timestamp = event.Timestamp
	}

	replyToID := ""
	if event.Message != nil && event.Message.Reference() != nil {
		replyToID = event.Message.Reference().MessageID
	}

	message := models.Message{
		ID:               event.ID,
		ChannelID:        event.ChannelID,
		GuildID:          event.GuildID,
		AuthorID:         event.Author.ID,
		Author:           event.Author.Username,
		MentionedUserIDs: mentionedUserIDs,
		Content:          content,
		Timestamp:        timestamp,
		ReplyToID:        replyToID,
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
	r.persistEdge(ctx, models.EdgeInput{
		FromType: "user",
		FromID:   message.AuthorID,
		EdgeType: "SENT",
		ToType:   "message",
		ToID:     message.ID,
	}, message.Timestamp, message.ID)

	extractionCtx, err := r.retriever.RetrieveForExtraction(ctx, memory.RetrieveForExtractionInput{
		ChannelID: message.ChannelID,
		SpeakerID: message.AuthorID,
		ReplyToID: message.ReplyToID,
	})
	if err != nil {
		log.Printf("event=extract_context_error message_id=%q err=%q", message.ID, err.Error())
		extractionCtx = models.ExtractionContext{}
	}

	topicCandidates, err := r.extractor.ExtractTopics(ctx, message, extractionCtx)
	if err != nil {
		log.Printf("event=topic_extract_error message_id=%q err=%q", message.ID, err.Error())
	}
	resolvedNewTopics := r.resolveAndPersistTopics(ctx, message, topicCandidates)

	factCandidates, err := r.extractor.ExtractFacts(ctx, message, resolvedNewTopics, extractionCtx.RecentTopics, extractionCtx)
	if err != nil {
		log.Printf("event=fact_extract_error message_id=%q err=%q", message.ID, err.Error())
	}
	r.resolveAndPersistFacts(ctx, message, factCandidates, resolvedNewTopics, extractionCtx.RecentTopics)

	if err := r.store.Flush(); err != nil {
		log.Printf("event=store_flush_error message_id=%q err=%q", message.ID, err.Error())
	}

	bundle, err := r.retriever.Retrieve(ctx, memory.RetrieveInput{
		ChannelID: message.ChannelID,
		SpeakerID: message.AuthorID,
	})
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

func (r *Runtime) resolveAndPersistTopics(ctx context.Context, sourceMessage models.Message, topicCandidates []string) []models.Topic {
	resolved := make([]models.Topic, 0, len(topicCandidates))
	seen := make(map[int64]struct{}, len(topicCandidates))
	for _, topicName := range topicCandidates {
		if len(resolved) >= maxTopicsPerMessage {
			break
		}

		topicName = normalizeTopicForResolution(topicName)
		if topicName == "" || isGenericTopic(topicName) {
			continue
		}

		topic, err := r.store.UpsertTopic(ctx, topicName, sourceMessage.Timestamp)
		if err != nil {
			log.Printf("event=topic_upsert_error message_id=%q topic=%q err=%q", sourceMessage.ID, topicName, err.Error())
			continue
		}
		if _, ok := seen[topic.ID]; ok {
			continue
		}
		seen[topic.ID] = struct{}{}
		resolved = append(resolved, topic)

		if err := r.store.LinkMessageTopic(ctx, sourceMessage.ID, topic.ID); err != nil {
			log.Printf("event=topic_link_error message_id=%q topic_id=%d err=%q", sourceMessage.ID, topic.ID, err.Error())
			continue
		}
		r.persistEdge(ctx, models.EdgeInput{
			FromType: "message",
			FromID:   sourceMessage.ID,
			EdgeType: "MENTIONS_TOPIC",
			ToType:   "topic",
			ToID:     strconv.FormatInt(topic.ID, 10),
		}, sourceMessage.Timestamp, sourceMessage.ID)
	}
	return resolved
}

func (r *Runtime) resolveAndPersistFacts(ctx context.Context, sourceMessage models.Message, factCandidates []memory.FactCandidate, newTopics, contextTopics []models.Topic) {
	for _, candidate := range factCandidates {
		aboutType, aboutID, ok := resolveAboutRef(candidate.AboutRef, sourceMessage.AuthorID, newTopics, contextTopics)
		if !ok {
			continue
		}
		if aboutType == "user" && shouldPreferTopicFact(candidate.Kind) {
			if topicType, topicID, topicOK := firstTopicTarget(newTopics, contextTopics); topicOK {
				aboutType = topicType
				aboutID = topicID
			}
		}

		fact, err := r.store.UpsertFactFromMessage(ctx, models.FactInput{
			DiscordUserID: sourceMessage.AuthorID,
			Kind:          candidate.Kind,
			ValueText:     candidate.ValueText,
			AboutType:     aboutType,
			AboutID:       aboutID,
			Confidence:    candidate.Confidence,
		}, sourceMessage.ID, r.cfg.OllamaExtractModel, sourceMessage.Timestamp)
		if err != nil {
			log.Printf("event=fact_upsert_error message_id=%q kind=%q err=%q", sourceMessage.ID, candidate.Kind, err.Error())
			continue
		}

		factID := strconv.FormatInt(fact.ID, 10)
		r.persistEdge(ctx, models.EdgeInput{
			FromType: "message",
			FromID:   sourceMessage.ID,
			EdgeType: "DERIVED_FACT",
			ToType:   "fact",
			ToID:     factID,
		}, sourceMessage.Timestamp, sourceMessage.ID)
		r.persistEdge(ctx, models.EdgeInput{
			FromType: "fact",
			FromID:   factID,
			EdgeType: "FACT_FOR_USER",
			ToType:   "user",
			ToID:     sourceMessage.AuthorID,
		}, sourceMessage.Timestamp, sourceMessage.ID)
		if fact.AboutType == "topic" {
			r.persistEdge(ctx, models.EdgeInput{
				FromType: "fact",
				FromID:   factID,
				EdgeType: "FACT_ABOUT_TOPIC",
				ToType:   "topic",
				ToID:     fact.AboutID,
			}, sourceMessage.Timestamp, sourceMessage.ID)
		}
	}
}

func (r *Runtime) persistEdge(ctx context.Context, input models.EdgeInput, observedAt time.Time, messageID string) {
	if _, err := r.store.UpsertEdge(ctx, input, observedAt); err != nil {
		log.Printf("event=edge_upsert_error message_id=%q edge_type=%q err=%q", messageID, input.EdgeType, err.Error())
	}
}

func resolveAboutRef(rawRef, speakerID string, newTopics, contextTopics []models.Topic) (string, string, bool) {
	ref := strings.ToLower(strings.TrimSpace(rawRef))
	switch {
	case ref == "user":
		return "user", speakerID, true
	case strings.HasPrefix(ref, "new_topic:"):
		index, ok := parseTopicRefIndex(strings.TrimPrefix(ref, "new_topic:"))
		if !ok || index >= len(newTopics) {
			return "", "", false
		}
		return "topic", strconv.FormatInt(newTopics[index].ID, 10), true
	case strings.HasPrefix(ref, "context_topic:"):
		index, ok := parseTopicRefIndex(strings.TrimPrefix(ref, "context_topic:"))
		if !ok || index >= len(contextTopics) {
			return "", "", false
		}
		return "topic", strconv.FormatInt(contextTopics[index].ID, 10), true
	default:
		return "", "", false
	}
}

func parseTopicRefIndex(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 {
		return 0, false
	}
	return index, true
}

func normalizeTopicForResolution(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	input = strings.Join(strings.Fields(input), " ")
	input = strings.TrimPrefix(input, "the ")
	return input
}

func isGenericTopic(topic string) bool {
	switch topic {
	case "", "technology", "tech", "software", "development", "project", "projects", "goal", "goals", "coding", "assistant", "conversation":
		return true
	default:
		return false
	}
}

func shouldPreferTopicFact(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "goal", "project", "status", "preference":
		return true
	default:
		return false
	}
}

func firstTopicTarget(newTopics, contextTopics []models.Topic) (string, string, bool) {
	if len(newTopics) > 0 {
		return "topic", strconv.FormatInt(newTopics[0].ID, 10), true
	}
	if len(contextTopics) > 0 {
		return "topic", strconv.FormatInt(contextTopics[0].ID, 10), true
	}
	return "", "", false
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
