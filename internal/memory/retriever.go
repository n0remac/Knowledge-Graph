package memory

import (
	"context"
	"fmt"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/store"
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
}

func NewRetriever(db *store.Store, recentMessageLimit, factLimit, topicLimit int) *Retriever {
	return &Retriever{
		store:              db,
		recentMessageLimit: recentMessageLimit,
		factLimit:          factLimit,
		topicLimit:         topicLimit,
	}
}

func (r *Retriever) RetrieveForExtraction(ctx context.Context, input RetrieveForExtractionInput) (models.ExtractionContext, error) {
	recentMessages, err := r.store.GetRecentMessages(ctx, input.ChannelID, r.recentMessageLimit)
	if err != nil {
		return models.ExtractionContext{}, err
	}

	recentTopics, err := r.store.GetRecentTopicsForChannel(ctx, input.ChannelID, r.recentMessageLimit, r.topicLimit)
	if err != nil {
		return models.ExtractionContext{}, err
	}

	recentDurableFacts, err := r.store.GetDurableFactsForDiscordUser(ctx, input.SpeakerID, r.factLimit)
	if err != nil {
		return models.ExtractionContext{}, err
	}

	var replyMessage *models.Message
	if input.ReplyToID != "" {
		message, ok := r.store.GetMessageByID(ctx, input.ReplyToID)
		if ok {
			replyMessage = &message
		}
	}

	return models.ExtractionContext{
		RecentMessages:     recentMessages,
		RecentTopics:       recentTopics,
		RecentDurableFacts: recentDurableFacts,
		ReplyMessage:       replyMessage,
	}, nil
}

func (r *Retriever) Retrieve(ctx context.Context, input RetrieveInput) (models.RetrievalBundle, error) {
	recentMessages, err := r.store.GetRecentMessages(ctx, input.ChannelID, r.recentMessageLimit)
	if err != nil {
		return models.RetrievalBundle{}, err
	}

	topics, err := r.store.GetRecentTopicsForChannel(ctx, input.ChannelID, r.recentMessageLimit, r.topicLimit)
	if err != nil {
		return models.RetrievalBundle{}, err
	}

	facts, err := r.store.GetDurableFactsForDiscordUser(ctx, input.SpeakerID, r.factLimit*3)
	if err != nil {
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

	return models.RetrievalBundle{
		RecentMessages: recentMessages,
		UserFacts:      userFacts,
		TopicFacts:     topicFacts,
		Topics:         topics,
	}, nil
}
