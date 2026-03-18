package conversation

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

func TestStorePersistsConversationArtifacts(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cs, err := NewStore(storePath, nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer func() {
		if err := cs.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	ctx := context.Background()
	first, err := cs.SaveRawMessage(ctx, models.RawMessage{
		MessageID:       "m1",
		ConversationID:  "c1",
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "Can you explain the pipeline?",
		TimestampUnixMs: 1000,
	})
	if err != nil {
		t.Fatalf("SaveRawMessage(first) error = %v", err)
	}
	if first.SequenceNumber != 1 {
		t.Fatalf("first.SequenceNumber = %d, want 1", first.SequenceNumber)
	}

	second, err := cs.SaveRawMessage(ctx, models.RawMessage{
		MessageID:        "m2",
		ConversationID:   "c1",
		AuthorID:         "assistant-1",
		AuthorRole:       "assistant",
		Content:          "It updates a rolling state every turn.",
		TimestampUnixMs:  2000,
		ReplyToMessageID: "m1",
	})
	if err != nil {
		t.Fatalf("SaveRawMessage(second) error = %v", err)
	}
	if second.SequenceNumber != 2 {
		t.Fatalf("second.SequenceNumber = %d, want 2", second.SequenceNumber)
	}

	if _, err := cs.SaveMessageExtraction(ctx, models.MessageExtraction{
		MessageID:           second.MessageID,
		ConversationID:      second.ConversationID,
		Claims:              []models.Claim{{Subject: "system", Predicate: "updates", Object: "rolling state", SourceMessageID: second.MessageID}},
		ActiveTopics:        []models.TopicRef{{Name: "rolling state"}},
		MessageSummary:      "The assistant explains the live state update loop.",
		ClaimsStatus:        "ok",
		TopicsStatus:        "ok",
		SummaryStatus:       "ok",
		ClaimsModelVersion:  "test-model",
		SummaryModelVersion: "test-model",
	}); err != nil {
		t.Fatalf("SaveMessageExtraction() error = %v", err)
	}

	state, err := cs.SaveWorkingState(ctx, models.WorkingState{
		ConversationID:       "c1",
		LastUpdatedMessageID: "m2",
		StateVersion:         2,
		RollingSummary:       "The conversation is about the live state pipeline.",
		ActiveTopics:         []models.TopicState{{Name: "rolling state", Salience: 0.95, Status: "active", LastSeenIn: "m2"}},
		RecentMessageIDs:     []string{"m1", "m2"},
		RecentExtractionIDs:  []string{"m2"},
	})
	if err != nil {
		t.Fatalf("SaveWorkingState() error = %v", err)
	}
	if state.StateVersion != 2 {
		t.Fatalf("state.StateVersion = %d, want 2", state.StateVersion)
	}

	if _, err := cs.SaveResponseContext(ctx, models.ResponseContextArtifact{
		MessageID:           "m2",
		ConversationID:      "c1",
		WorkingStateVersion: 2,
		Brief:               "Current Conversation\nThe conversation is about the live state pipeline.",
		RecentMessageIDs:    []string{"m1", "m2"},
		CreatedAtUnixMs:     2000,
	}); err != nil {
		t.Fatalf("SaveResponseContext() error = %v", err)
	}

	snapshot := cs.Snapshot(SnapshotOptions{RecentMessages: 5, RecentExtractions: 5})
	if len(snapshot.Conversations) != 1 {
		t.Fatalf("len(snapshot.Conversations) = %d, want 1", len(snapshot.Conversations))
	}

	conversation := snapshot.Conversations[0]
	if conversation.ConversationID != "c1" {
		t.Fatalf("ConversationID = %q, want c1", conversation.ConversationID)
	}
	if conversation.LatestMessage.MessageID != "m2" {
		t.Fatalf("LatestMessage.MessageID = %q, want m2", conversation.LatestMessage.MessageID)
	}
	if conversation.WorkingState == nil || conversation.WorkingState.RollingSummary == "" {
		t.Fatalf("working state missing from snapshot: %#v", conversation.WorkingState)
	}
	if conversation.LatestResponseContext == nil || conversation.LatestResponseContext.Brief == "" {
		t.Fatalf("latest response context missing from snapshot: %#v", conversation.LatestResponseContext)
	}
	if len(conversation.RecentExtractions) != 1 {
		t.Fatalf("len(RecentExtractions) = %d, want 1", len(conversation.RecentExtractions))
	}
}
