package memory

import (
	"context"
	"fmt"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/store"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

type RetrieveInput struct {
	ChannelID string
	SpeakerID string
}

type RetrieveForExtractionInput struct {
	ChannelID string
	SpeakerID string
	ReplyToID string
}

type Retriever struct {
	store              *store.Store
	recentMessageLimit int
	factLimit          int
	topicLimit         int
	telemetry          *telemetry.Manager
}

func NewRetriever(db *store.Store, recentMessageLimit, factLimit, topicLimit int, manager *telemetry.Manager) *Retriever {
	return &Retriever{
		store:              db,
		recentMessageLimit: recentMessageLimit,
		factLimit:          factLimit,
		topicLimit:         topicLimit,
		telemetry:          manager,
	}
}

func (r *Retriever) RetrieveForExtraction(ctx context.Context, input RetrieveForExtractionInput) (models.ExtractionContext, error) {
	recentMessages, err := r.store.GetRecentMessages(ctx, input.ChannelID, r.recentMessageLimit)
	if err != nil {
		r.emit(ctx, "retrieve_for_extraction_error", "retrieval failed for extraction context", map[string]any{
			"input": input,
			"error": err.Error(),
		})
		return models.ExtractionContext{}, err
	}

	recentTopics, err := r.store.GetRecentTopicsForChannel(ctx, input.ChannelID, r.recentMessageLimit, r.topicLimit)
	if err != nil {
		r.emit(ctx, "retrieve_for_extraction_error", "retrieval failed for extraction topics", map[string]any{
			"input": input,
			"error": err.Error(),
		})
		return models.ExtractionContext{}, err
	}

	recentDurableFacts, err := r.store.GetDurableFactsForDiscordUser(ctx, input.SpeakerID, r.factLimit)
	if err != nil {
		r.emit(ctx, "retrieve_for_extraction_error", "retrieval failed for extraction durable facts", map[string]any{
			"input": input,
			"error": err.Error(),
		})
		return models.ExtractionContext{}, err
	}

	var replyMessage *models.Message
	if input.ReplyToID != "" {
		message, ok := r.store.GetMessageByID(ctx, input.ReplyToID)
		if ok {
			replyMessage = &message
		}
	}

	output := models.ExtractionContext{
		RecentMessages:     recentMessages,
		RecentTopics:       recentTopics,
		RecentDurableFacts: recentDurableFacts,
		ReplyMessage:       replyMessage,
	}
	r.emit(ctx, "retrieve_for_extraction", "retrieval assembled extraction context", map[string]any{
		"input": input,
		"counts": map[string]any{
			"recent_messages":      len(recentMessages),
			"recent_topics":        len(recentTopics),
			"recent_durable_facts": len(recentDurableFacts),
			"has_reply_message":    replyMessage != nil,
		},
		"recent_messages":      recentMessages,
		"recent_topics":        recentTopics,
		"recent_durable_facts": recentDurableFacts,
		"reply_message":        replyMessage,
	})
	return output, nil
}

func (r *Retriever) Retrieve(ctx context.Context, input RetrieveInput) (models.RetrievalBundle, error) {
	recentMessages, err := r.store.GetRecentMessages(ctx, input.ChannelID, r.recentMessageLimit)
	if err != nil {
		r.emit(ctx, "retrieve_for_generation_error", "retrieval failed for generation messages", map[string]any{
			"input": input,
			"error": err.Error(),
		})
		return models.RetrievalBundle{}, err
	}

	topics, err := r.store.GetRecentTopicsForChannel(ctx, input.ChannelID, r.recentMessageLimit, r.topicLimit)
	if err != nil {
		r.emit(ctx, "retrieve_for_generation_error", "retrieval failed for generation topics", map[string]any{
			"input": input,
			"error": err.Error(),
		})
		return models.RetrievalBundle{}, err
	}

	facts, err := r.store.GetDurableFactsForDiscordUser(ctx, input.SpeakerID, r.factLimit*3)
	if err != nil {
		r.emit(ctx, "retrieve_for_generation_error", "retrieval failed for generation facts", map[string]any{
			"input": input,
			"error": err.Error(),
		})
		return models.RetrievalBundle{}, err
	}

	activeTopicIDs := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		activeTopicIDs[fmt.Sprintf("%d", topic.ID)] = struct{}{}
	}

	userFacts := make([]models.Fact, 0, r.factLimit)
	topicFacts := make([]models.Fact, 0, r.factLimit)
	for _, fact := range facts {
		switch {
		case fact.AboutType == "user" && fact.AboutID == input.SpeakerID:
			if len(userFacts) < r.factLimit {
				userFacts = append(userFacts, fact)
			}
		case fact.AboutType == "topic":
			if _, ok := activeTopicIDs[fact.AboutID]; ok && len(topicFacts) < r.factLimit {
				topicFacts = append(topicFacts, fact)
			}
		}
	}

	output := models.RetrievalBundle{
		RecentMessages: recentMessages,
		UserFacts:      userFacts,
		TopicFacts:     topicFacts,
		Topics:         topics,
	}
	r.emit(ctx, "retrieve_for_generation", "retrieval assembled generation bundle", map[string]any{
		"input": input,
		"counts": map[string]any{
			"recent_messages": len(recentMessages),
			"topics":          len(topics),
			"user_facts":      len(userFacts),
			"topic_facts":     len(topicFacts),
		},
		"recent_messages": recentMessages,
		"topics":          topics,
		"user_facts":      userFacts,
		"topic_facts":     topicFacts,
	})
	return output, nil
}

func (r *Retriever) emit(ctx context.Context, kind, summary string, payload map[string]any) {
	if r == nil || r.telemetry == nil {
		return
	}
	r.telemetry.Emit(ctx, telemetry.StageRetrieval, kind, summary, payload)
}
