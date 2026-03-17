package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/n0remac/Knowledge-Graph/internal/conversation"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

func TestConversationDataHandler(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cs, err := conversation.NewStore(storePath, nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer func() {
		if err := cs.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	ctx := context.Background()
	message, err := cs.SaveRawMessage(ctx, models.RawMessage{
		MessageID:       "trace-1",
		ConversationID:  "channel-1",
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "Tell me about the rolling summary.",
		TimestampUnixMs: 1700,
	})
	if err != nil {
		t.Fatalf("SaveRawMessage() error = %v", err)
	}
	if _, err := cs.SaveMessageExtraction(ctx, models.MessageExtraction{
		MessageID:       message.MessageID,
		ConversationID:  message.ConversationID,
		ActiveTopics:    []models.TopicRef{{Name: "rolling summary"}},
		MessageSummary:  "The user asks about the rolling summary.",
		ClaimsStatus:    "ok",
		QuestionsStatus: "ok",
		TopicsStatus:    "ok",
		PronounsStatus:  "ok",
		SummaryStatus:   "ok",
	}); err != nil {
		t.Fatalf("SaveMessageExtraction() error = %v", err)
	}
	if _, err := cs.SaveWorkingState(ctx, models.WorkingState{
		ConversationID:       message.ConversationID,
		LastUpdatedMessageID: message.MessageID,
		StateVersion:         1,
		RollingSummary:       "The conversation is about the rolling summary.",
		ActiveTopics:         []models.TopicState{{Name: "rolling summary", Salience: 0.9, Status: "active", LastSeenIn: message.MessageID}},
		RecentMessageIDs:     []string{message.MessageID},
		RecentExtractionIDs:  []string{message.MessageID},
	}); err != nil {
		t.Fatalf("SaveWorkingState() error = %v", err)
	}
	if _, err := cs.SaveResponseContext(ctx, models.ResponseContextArtifact{
		MessageID:           message.MessageID,
		ConversationID:      message.ConversationID,
		WorkingStateVersion: 1,
		Brief:               "Current Conversation\nThe conversation is about the rolling summary.",
		RecentMessageIDs:    []string{message.MessageID},
		CreatedAtUnixMs:     1700,
	}); err != nil {
		t.Fatalf("SaveResponseContext() error = %v", err)
	}

	telemetryDir := t.TempDir()
	traceDir := filepath.Join(telemetryDir, "trace-1")
	if err := writeFixtureJSON(filepath.Join(traceDir, "trace.json"), telemetry.TraceIndex{TraceID: "trace-1", Status: "completed"}); err != nil {
		t.Fatalf("write trace index: %v", err)
	}
	if err := writeFixtureJSON(filepath.Join(traceDir, "summary.json"), telemetry.TraceSummary{TraceID: "trace-1", Status: "completed"}); err != nil {
		t.Fatalf("write trace summary: %v", err)
	}
	if err := writeFixtureJSON(filepath.Join(traceDir, "001_runtime_message_received.json"), map[string]any{"ok": true}); err != nil {
		t.Fatalf("write event artifact: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/conversation/data", nil)
	recorder := httptest.NewRecorder()

	ConversationDataHandler(cs, telemetryDir).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var payload ConversationData
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.Conversations) != 1 {
		t.Fatalf("len(payload.Conversations) = %d, want 1", len(payload.Conversations))
	}
	if payload.Conversations[0].LatestTrace == nil || payload.Conversations[0].LatestTrace.TraceID != "trace-1" {
		t.Fatalf("latest trace missing from response: %#v", payload.Conversations[0].LatestTrace)
	}
}

func writeFixtureJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}
