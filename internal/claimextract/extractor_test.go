package claimextract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

func TestExtractorExtractsClaims(t *testing.T) {
	t.Parallel()

	server := newFakeClaimExtractorServer(t, `{"message":{"content":"{\"claims\":[{\"subject\":\"bot\",\"predicate\":\"stores\",\"object\":\"memory\"}]}"}}`)
	t.Cleanup(server.Close)

	extractor := New(ollama.NewClient(server.URL, 2*time.Second), "test-model", telemetry.StageConversation, nil)
	result := extractor.Extract(context.Background(), Input{
		CurrentMessage: models.RawMessage{
			MessageID:      "message-1",
			AuthorRole:     "user",
			AuthorID:       "user-1",
			Content:        "The bot stores memory.",
			ConversationID: "conversation-1",
		},
	})

	if result.Err != nil {
		t.Fatalf("Extract() error = %v", result.Err)
	}
	if result.Status != "ok" {
		t.Fatalf("result.Status = %q, want ok", result.Status)
	}
	if len(result.Claims) != 1 {
		t.Fatalf("len(result.Claims) = %d, want 1", len(result.Claims))
	}
	if result.Claims[0].SourceMessageID != "message-1" {
		t.Fatalf("SourceMessageID = %q, want message-1", result.Claims[0].SourceMessageID)
	}
}

func TestExtractorReturnsPartialForDroppedClaims(t *testing.T) {
	t.Parallel()

	server := newFakeClaimExtractorServer(t, `{"message":{"content":"{\"claims\":[{\"subject\":\"bot\",\"predicate\":\"stores\",\"object\":\"memory\"},{\"subject\":\"\",\"predicate\":\"drops\",\"object\":\"bad\"}]}"}}`)
	t.Cleanup(server.Close)

	extractor := New(ollama.NewClient(server.URL, 2*time.Second), "test-model", telemetry.StageMemory, nil)
	result := extractor.Extract(context.Background(), Input{
		CurrentMessage: models.RawMessage{
			MessageID:      "message-1",
			AuthorRole:     "user",
			AuthorID:       "user-1",
			Content:        "The bot stores memory.",
			ConversationID: "conversation-1",
		},
	})

	if result.Err != nil {
		t.Fatalf("Extract() error = %v", result.Err)
	}
	if result.Status != "partial" {
		t.Fatalf("result.Status = %q, want partial", result.Status)
	}
	if len(result.Claims) != 1 {
		t.Fatalf("len(result.Claims) = %d, want 1", len(result.Claims))
	}
}

func TestExtractorFailsOnInvalidPayload(t *testing.T) {
	t.Parallel()

	server := newFakeClaimExtractorServer(t, `{"message":{"content":"not-json"}}`)
	t.Cleanup(server.Close)

	extractor := New(ollama.NewClient(server.URL, 2*time.Second), "test-model", telemetry.StageMemory, nil)
	result := extractor.Extract(context.Background(), Input{
		CurrentMessage: models.RawMessage{
			MessageID:      "message-1",
			AuthorRole:     "user",
			AuthorID:       "user-1",
			Content:        "The bot stores memory.",
			ConversationID: "conversation-1",
		},
	})

	if result.Err == nil {
		t.Fatal("Extract() error = nil, want parse error")
	}
	if result.Status != "failed" {
		t.Fatalf("result.Status = %q, want failed", result.Status)
	}
}

func newFakeClaimExtractorServer(t *testing.T, raw string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("json.Unmarshal(fake response) error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
}
