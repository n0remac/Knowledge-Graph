package web

import (
	"fmt"

	"github.com/n0remac/Knowledge-Graph/internal/conversation"
)

func BuildConversationData(cs *conversation.Store, telemetryBaseDir string) (ConversationData, error) {
	if cs == nil {
		return ConversationData{}, fmt.Errorf("conversation store unavailable")
	}

	snapshot := cs.Snapshot(conversation.SnapshotOptions{
		RecentMessages:    10,
		RecentExtractions: 10,
	})

	data := ConversationData{
		Conversations: make([]ConversationView, 0, len(snapshot.Conversations)),
	}
	for _, convo := range snapshot.Conversations {
		view := ConversationView{
			ConversationID:        convo.ConversationID,
			MessageCount:          convo.MessageCount,
			LatestMessage:         convo.LatestMessage,
			WorkingState:          convo.WorkingState,
			RecentMessages:        convo.RecentMessages,
			RecentExtractions:     convo.RecentExtractions,
			LatestResponseContext: convo.LatestResponseContext,
		}
		if trace, err := loadTraceArtifactBundle(telemetryBaseDir, convo.LatestMessage.MessageID); err == nil {
			view.LatestTrace = trace
		}
		data.Conversations = append(data.Conversations, view)
	}

	return data, nil
}
