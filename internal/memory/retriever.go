package memory

import (
	"context"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/store"
)

type RetrieveInput struct {
	ChannelID       string
	SpeakerID       string
	MentionedUserID []string
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

func (r *Retriever) Retrieve(ctx context.Context, input RetrieveInput) (models.RetrievalBundle, error) {
	recentMessages, err := r.store.GetRecentMessages(ctx, input.ChannelID, r.recentMessageLimit)
	if err != nil {
		return models.RetrievalBundle{}, err
	}

	subjectIDs := []string{input.SpeakerID}
	subjectIDs = append(subjectIDs, input.MentionedUserID...)
	facts, err := r.store.GetFactsForSubjects(ctx, subjectIDs, r.factLimit)
	if err != nil {
		return models.RetrievalBundle{}, err
	}

	topics, err := r.store.GetRecentTopicsForChannel(ctx, input.ChannelID, r.recentMessageLimit, r.topicLimit)
	if err != nil {
		return models.RetrievalBundle{}, err
	}

	return models.RetrievalBundle{
		RecentMessages: recentMessages,
		Facts:          facts,
		Topics:         topics,
	}, nil
}
