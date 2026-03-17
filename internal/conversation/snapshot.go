package conversation

import (
	"sort"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

type SnapshotOptions struct {
	RecentMessages    int
	RecentExtractions int
}

type Snapshot struct {
	Conversations []ConversationSnapshot `json:"conversations"`
}

type ConversationSnapshot struct {
	ConversationID        string                          `json:"conversation_id"`
	MessageCount          int                             `json:"message_count"`
	LatestMessage         models.RawMessage               `json:"latest_message"`
	WorkingState          *models.WorkingState            `json:"working_state,omitempty"`
	RecentMessages        []models.RawMessage             `json:"recent_messages,omitempty"`
	RecentExtractions     []models.MessageExtraction      `json:"recent_extractions,omitempty"`
	LatestResponseContext *models.ResponseContextArtifact `json:"latest_response_context,omitempty"`
}

func (s *Store) Snapshot(options SnapshotOptions) Snapshot {
	if options.RecentMessages <= 0 {
		options.RecentMessages = 10
	}
	if options.RecentExtractions <= 0 {
		options.RecentExtractions = 10
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	conversations := make([]ConversationSnapshot, 0, len(s.data.ConversationMessageIDs))
	for conversationID, messageIDs := range s.data.ConversationMessageIDs {
		if len(messageIDs) == 0 {
			continue
		}

		latestMessage := s.data.RawMessages[messageIDs[len(messageIDs)-1]]
		recentMessages := snapshotRecentMessages(s.data.RawMessages, messageIDs, options.RecentMessages)
		recentExtractions := snapshotRecentExtractions(s.data.MessageExtractions, messageIDs, options.RecentExtractions)

		var workingState *models.WorkingState
		if state, ok := s.data.WorkingStates[conversationID]; ok {
			copied := state
			workingState = &copied
		}

		var latestResponseContext *models.ResponseContextArtifact
		if messageID := s.data.LatestResponseContextByConversation[conversationID]; messageID != "" {
			if artifact, ok := s.data.ResponseContexts[messageID]; ok {
				copied := artifact
				latestResponseContext = &copied
			}
		}

		conversations = append(conversations, ConversationSnapshot{
			ConversationID:        conversationID,
			MessageCount:          len(messageIDs),
			LatestMessage:         latestMessage,
			WorkingState:          workingState,
			RecentMessages:        recentMessages,
			RecentExtractions:     recentExtractions,
			LatestResponseContext: latestResponseContext,
		})
	}

	sort.Slice(conversations, func(i, j int) bool {
		if conversations[i].LatestMessage.TimestampUnixMs == conversations[j].LatestMessage.TimestampUnixMs {
			return conversations[i].ConversationID < conversations[j].ConversationID
		}
		return conversations[i].LatestMessage.TimestampUnixMs > conversations[j].LatestMessage.TimestampUnixMs
	})

	return Snapshot{Conversations: conversations}
}

func snapshotRecentMessages(all map[string]models.RawMessage, messageIDs []string, limit int) []models.RawMessage {
	start := 0
	if len(messageIDs) > limit {
		start = len(messageIDs) - limit
	}
	out := make([]models.RawMessage, 0, len(messageIDs)-start)
	for _, messageID := range messageIDs[start:] {
		out = append(out, all[messageID])
	}
	return out
}

func snapshotRecentExtractions(all map[string]models.MessageExtraction, messageIDs []string, limit int) []models.MessageExtraction {
	start := 0
	if len(messageIDs) > limit {
		start = len(messageIDs) - limit
	}
	out := make([]models.MessageExtraction, 0, len(messageIDs)-start)
	for _, messageID := range messageIDs[start:] {
		extraction, ok := all[messageID]
		if !ok {
			continue
		}
		out = append(out, extraction)
	}
	return out
}
