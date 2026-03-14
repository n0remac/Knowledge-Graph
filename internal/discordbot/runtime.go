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
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

const (
	maxDiscordReplyRunes = 2000
	maxTopicsPerMessage  = 2
	botTestingChannel    = "bot-testing"
)

type Runtime struct {
	cfg       config.Config
	store     *store.Store
	extractor *memory.Extractor
	retriever *memory.Retriever
	generator *generate.Generator
	session   *discordgo.Session
	telemetry *telemetry.Manager
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	session, err := discordgo.New("Bot " + cfg.DiscordBotToken)
	if err != nil {
		return nil, err
	}
	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	manager, err := telemetry.NewManager(cfg.Telemetry, session)
	if err != nil {
		return nil, err
	}

	db, err := store.NewGraph(cfg.GraphStorePath, manager)
	if err != nil {
		_ = manager.Close()
		return nil, err
	}
	llmClient := ollama.NewClient(cfg.OllamaBaseURL, cfg.RequestTimeout)
	extractor := memory.NewExtractor(llmClient, cfg.OllamaExtractModel, manager)
	retriever := memory.NewRetriever(db, cfg.RecentMessageLimit, cfg.RecallFactLimit, cfg.RecallTopicLimit, manager)
	generator := generate.NewGenerator(llmClient, cfg.OllamaChatModel, cfg.Persona, manager)

	runtime := &Runtime{
		cfg:       cfg,
		store:     db,
		extractor: extractor,
		retriever: retriever,
		generator: generator,
		session:   session,
		telemetry: manager,
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
	if r.telemetry != nil {
		defer func() {
			if err := r.telemetry.Close(); err != nil {
				log.Printf("telemetry shutdown error: %v", err)
			}
		}()
	}
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

	if !isBotTestingChannel(session, event.ChannelID) {
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
	ctx = telemetry.WithTraceID(ctx, message.ID)

	r.emit(ctx, telemetry.StageRuntime, "message_received", "discord message received", map[string]any{
		"message": map[string]any{
			"id":                  message.ID,
			"channel_id":          message.ChannelID,
			"guild_id":            message.GuildID,
			"author_id":           message.AuthorID,
			"author_username":     event.Author.Username,
			"author_display_name": event.Author.GlobalName,
			"content":             message.Content,
			"reply_to_id":         message.ReplyToID,
			"mentioned_user_ids":  message.MentionedUserIDs,
			"timestamp":           message.Timestamp,
		},
	})

	if err := r.store.UpsertUser(ctx, message.AuthorID, event.Author.Username, event.Author.GlobalName, message.Timestamp); err != nil {
		log.Printf("event=store_user_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "store_user_error", "failed to upsert author", err, nil)
	}
	if err := r.store.SaveMessage(ctx, message); err != nil {
		log.Printf("event=store_message_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "store_message_error", "failed to save source message", err, nil)
	} else {
		r.emit(ctx, telemetry.StageRuntime, "message_saved", "source message persisted", map[string]any{
			"message_id": message.ID,
		})
	}
	if _, ok := r.persistEdge(ctx, models.EdgeInput{
		FromType: "user",
		FromID:   message.AuthorID,
		EdgeType: "SENT",
		ToType:   "message",
		ToID:     message.ID,
	}, message.Timestamp, message.ID); ok {
		r.emit(ctx, telemetry.StageRuntime, "sent_edge_persisted", "author-to-message sent edge persisted", map[string]any{
			"message_id": message.ID,
			"author_id":  message.AuthorID,
		})
	}

	extractionCtx, err := r.retriever.RetrieveForExtraction(ctx, memory.RetrieveForExtractionInput{
		ChannelID: message.ChannelID,
		SpeakerID: message.AuthorID,
		ReplyToID: message.ReplyToID,
	})
	if err != nil {
		log.Printf("event=extract_context_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "extract_context_error", "failed to build extraction context", err, nil)
		extractionCtx = models.ExtractionContext{}
	} else {
		r.emit(ctx, telemetry.StageRuntime, "extraction_context_retrieved", "extraction context retrieved", map[string]any{
			"counts": map[string]any{
				"recent_messages":      len(extractionCtx.RecentMessages),
				"recent_topics":        len(extractionCtx.RecentTopics),
				"recent_durable_facts": len(extractionCtx.RecentDurableFacts),
				"has_reply_message":    extractionCtx.ReplyMessage != nil,
			},
		})
	}

	topicCandidates, err := r.extractor.ExtractTopics(ctx, message, extractionCtx)
	if err != nil {
		log.Printf("event=topic_extract_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "topic_extract_error", "topic extraction failed", err, nil)
	}
	resolvedNewTopics := r.resolveAndPersistTopics(ctx, message, topicCandidates)

	factCandidates, err := r.extractor.ExtractFacts(ctx, message, resolvedNewTopics, extractionCtx.RecentTopics, extractionCtx)
	if err != nil {
		log.Printf("event=fact_extract_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "fact_extract_error", "fact extraction failed", err, nil)
	}
	r.resolveAndPersistFacts(ctx, message, factCandidates, resolvedNewTopics, extractionCtx.RecentTopics)

	if err := r.store.Flush(); err != nil {
		log.Printf("event=store_flush_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "store_flush_error", "graph store flush failed", err, nil)
	} else {
		r.emit(ctx, telemetry.StageRuntime, "store_flushed", "graph store flushed", map[string]any{
			"message_id": message.ID,
		})
	}

	bundle, err := r.retriever.Retrieve(ctx, memory.RetrieveInput{
		ChannelID: message.ChannelID,
		SpeakerID: message.AuthorID,
	})
	if err != nil {
		log.Printf("event=retrieve_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "retrieve_error", "generation retrieval failed", err, nil)
		bundle = models.RetrievalBundle{}
	} else {
		r.emit(ctx, telemetry.StageRuntime, "generation_bundle_retrieved", "generation bundle retrieved", map[string]any{
			"counts": map[string]any{
				"recent_messages": len(bundle.RecentMessages),
				"user_facts":      len(bundle.UserFacts),
				"topic_facts":     len(bundle.TopicFacts),
				"topics":          len(bundle.Topics),
			},
		})
	}

	if err := session.ChannelTyping(message.ChannelID); err != nil {
		log.Printf("event=typing_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "typing_error", "failed to send typing indicator", err, nil)
	}

	reply, err := r.generator.GenerateReply(ctx, message, bundle)
	if err != nil {
		log.Printf("event=generate_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "generate_error", "reply generation failed", err, nil)
		_, sendErr := session.ChannelMessageSend(message.ChannelID, "I couldn't generate a response right now.")
		if sendErr != nil {
			log.Printf("event=send_error message_id=%q err=%q", message.ID, sendErr.Error())
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
	if _, err := session.ChannelMessageSend(message.ChannelID, reply); err != nil {
		log.Printf("event=send_error message_id=%q err=%q", message.ID, err.Error())
		r.emitError(ctx, telemetry.StageRuntime, "send_error", "failed to send reply", err, map[string]any{
			"reply_length": len([]rune(reply)),
		})
		return
	}
	log.Printf("event=reply_sent source_message_id=%q reply_len=%d", message.ID, len([]rune(reply)))
	r.emit(ctx, telemetry.StageRuntime, "reply_sent", "reply sent to discord", map[string]any{
		"reply":        reply,
		"reply_length": len([]rune(reply)),
	})
}

func (r *Runtime) resolveAndPersistTopics(ctx context.Context, sourceMessage models.Message, topicCandidates []string) []models.Topic {
	resolved := make([]models.Topic, 0, len(topicCandidates))
	seen := make(map[int64]struct{}, len(topicCandidates))
	resolvedPayload := make([]map[string]any, 0, len(topicCandidates))
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
			r.emitError(ctx, telemetry.StageResolution, "topic_upsert_error", "failed to upsert topic", err, map[string]any{"topic_name": topicName})
			continue
		}
		if _, ok := seen[topic.ID]; ok {
			continue
		}
		seen[topic.ID] = struct{}{}
		resolved = append(resolved, topic)

		if err := r.store.LinkMessageTopic(ctx, sourceMessage.ID, topic.ID); err != nil {
			log.Printf("event=topic_link_error message_id=%q topic_id=%d err=%q", sourceMessage.ID, topic.ID, err.Error())
			r.emitError(ctx, telemetry.StageResolution, "topic_link_error", "failed to link message to topic", err, map[string]any{"topic_id": topic.ID})
			continue
		}
		r.persistEdge(ctx, models.EdgeInput{
			FromType: "message",
			FromID:   sourceMessage.ID,
			EdgeType: "MENTIONS_TOPIC",
			ToType:   "topic",
			ToID:     strconv.FormatInt(topic.ID, 10),
		}, sourceMessage.Timestamp, sourceMessage.ID)
		resolvedPayload = append(resolvedPayload, map[string]any{
			"id":   topic.ID,
			"name": topic.Name,
		})
	}
	r.emit(ctx, telemetry.StageResolution, "topics_resolved", "topic candidates resolved and linked", map[string]any{
		"topic_candidates": topicCandidates,
		"topics":           resolvedPayload,
	})
	return resolved
}

func (r *Runtime) resolveAndPersistFacts(ctx context.Context, sourceMessage models.Message, factCandidates []memory.FactCandidate, newTopics, contextTopics []models.Topic) {
	persisted := make([]map[string]any, 0, len(factCandidates))
	edges := make([]map[string]any, 0, len(factCandidates)*3)
	for _, candidate := range factCandidates {
		aboutType, aboutID, ok := resolveAboutRef(candidate.AboutRef, sourceMessage.AuthorID, newTopics, contextTopics)
		if !ok {
			r.emit(ctx, telemetry.StageResolution, "invalid_about_ref_error", "fact candidate referenced an invalid target", map[string]any{
				"error":     "invalid about_ref",
				"candidate": factCandidatePayloadMap(candidate),
			})
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
			r.emitError(ctx, telemetry.StageResolution, "fact_upsert_error", "failed to upsert fact candidate", err, map[string]any{
				"candidate": factCandidatePayloadMap(candidate),
			})
			continue
		}
		persisted = append(persisted, map[string]any{
			"id":              fact.ID,
			"kind":            fact.Kind,
			"value_text":      fact.ValueText,
			"about_type":      fact.AboutType,
			"about_id":        fact.AboutID,
			"confidence":      fact.Confidence,
			"status":          fact.Status,
			"candidate_input": factCandidatePayloadMap(candidate),
		})

		factID := strconv.FormatInt(fact.ID, 10)
		if edge, ok := r.persistEdge(ctx, models.EdgeInput{
			FromType: "message",
			FromID:   sourceMessage.ID,
			EdgeType: "DERIVED_FACT",
			ToType:   "fact",
			ToID:     factID,
		}, sourceMessage.Timestamp, sourceMessage.ID); ok {
			edges = append(edges, edgePayload(edge))
		}
		if edge, ok := r.persistEdge(ctx, models.EdgeInput{
			FromType: "fact",
			FromID:   factID,
			EdgeType: "FACT_FOR_USER",
			ToType:   "user",
			ToID:     sourceMessage.AuthorID,
		}, sourceMessage.Timestamp, sourceMessage.ID); ok {
			edges = append(edges, edgePayload(edge))
		}
		if fact.AboutType == "topic" {
			if edge, ok := r.persistEdge(ctx, models.EdgeInput{
				FromType: "fact",
				FromID:   factID,
				EdgeType: "FACT_ABOUT_TOPIC",
				ToType:   "topic",
				ToID:     fact.AboutID,
			}, sourceMessage.Timestamp, sourceMessage.ID); ok {
				edges = append(edges, edgePayload(edge))
			}
		}
	}
	r.emit(ctx, telemetry.StageResolution, "facts_resolved", "fact candidates resolved and persisted", map[string]any{
		"fact_candidates": factCandidatesPayload(factCandidates),
		"facts":           persisted,
		"edges":           edges,
	})
}

func (r *Runtime) persistEdge(ctx context.Context, input models.EdgeInput, observedAt time.Time, messageID string) (models.Edge, bool) {
	edge, err := r.store.UpsertEdge(ctx, input, observedAt)
	if err != nil {
		log.Printf("event=edge_upsert_error message_id=%q edge_type=%q err=%q", messageID, input.EdgeType, err.Error())
		r.emitError(ctx, telemetry.StageResolution, "edge_upsert_error", "failed to upsert edge", err, map[string]any{
			"edge_type": input.EdgeType,
			"input":     input,
		})
		return models.Edge{}, false
	}
	return edge, true
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

func factCandidatePayloadMap(candidate memory.FactCandidate) map[string]any {
	return map[string]any{
		"kind":       candidate.Kind,
		"about_ref":  candidate.AboutRef,
		"value_text": candidate.ValueText,
		"confidence": candidate.Confidence,
	}
}

func factCandidatesPayload(candidates []memory.FactCandidate) []map[string]any {
	out := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, factCandidatePayloadMap(candidate))
	}
	return out
}

func edgePayload(edge models.Edge) map[string]any {
	return map[string]any{
		"id":         edge.ID,
		"from_type":  edge.FromType,
		"from_id":    edge.FromID,
		"edge_type":  edge.EdgeType,
		"to_type":    edge.ToType,
		"to_id":      edge.ToID,
		"created_at": edge.CreatedAt,
		"last_seen":  edge.LastSeenAt,
	}
}
