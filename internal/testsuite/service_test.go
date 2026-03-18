package testsuite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/conversation"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

func TestServiceTranscriptCRUD(t *testing.T) {
	t.Parallel()

	server := newFakeOllamaServer(t, fakeOllamaConfig{})
	t.Cleanup(server.Close)
	service := newTestService(t, server.URL)

	created, err := service.SaveTranscript(Transcript{
		Name:        "Remember Name",
		Description: "Tracks whether the bot remembers a name.",
		Steps: []TranscriptStep{
			{Message: "My name is Sam."},
			{Message: "What is my name?"},
		},
	})
	if err != nil {
		t.Fatalf("SaveTranscript(create) error = %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created.ID is empty")
	}

	transcripts, err := service.ListTranscripts()
	if err != nil {
		t.Fatalf("ListTranscripts() error = %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("len(transcripts) = %d, want 1", len(transcripts))
	}

	created.Description = "Updated description."
	updated, err := service.SaveTranscript(created)
	if err != nil {
		t.Fatalf("SaveTranscript(update) error = %v", err)
	}
	if updated.Description != "Updated description." {
		t.Fatalf("updated.Description = %q, want updated value", updated.Description)
	}
	if updated.CreatedAtUnixMs == 0 || updated.UpdatedAtUnixMs == 0 {
		t.Fatalf("expected created/updated timestamps, got %#v", updated)
	}

	if err := service.DeleteTranscript(created.ID); err != nil {
		t.Fatalf("DeleteTranscript() error = %v", err)
	}
	if _, err := service.GetTranscript(created.ID); err == nil {
		t.Fatalf("GetTranscript() after delete returned nil error")
	}
}

func TestServiceRunTranscriptPersistsArtifacts(t *testing.T) {
	t.Parallel()

	server := newFakeOllamaServer(t, fakeOllamaConfig{})
	t.Cleanup(server.Close)
	service := newTestService(t, server.URL)

	transcript, err := service.SaveTranscript(Transcript{
		Name: "Name Recall",
		Steps: []TranscriptStep{
			{Message: "My name is Sam."},
			{Message: "What is my name?"},
		},
	})
	if err != nil {
		t.Fatalf("SaveTranscript() error = %v", err)
	}

	record, err := service.RunTranscript(context.Background(), RunRequest{
		TranscriptID: transcript.ID,
	})
	if err != nil {
		t.Fatalf("RunTranscript() error = %v", err)
	}
	if record.Status != "completed" {
		t.Fatalf("record.Status = %q, want completed (error=%q)", record.Status, record.Error)
	}
	if len(record.Steps) != 2 {
		t.Fatalf("len(record.Steps) = %d, want 2", len(record.Steps))
	}
	for i, step := range record.Steps {
		if step.ResponseBrief == "" {
			t.Fatalf("step %d response brief is empty", i+1)
		}
		if step.RollingSummary == "" {
			t.Fatalf("step %d rolling summary is empty", i+1)
		}
		if len(step.TraceIDs) == 0 {
			t.Fatalf("step %d trace ids are empty", i+1)
		}
	}
	if !strings.HasPrefix(record.StorePath, filepath.Join(service.cfg.BaseDir, "runs")) {
		t.Fatalf("record.StorePath = %q, want path under test suite base dir", record.StorePath)
	}

	saved, err := service.GetRun(record.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if saved.Status != "completed" {
		t.Fatalf("saved.Status = %q, want completed", saved.Status)
	}

	store, err := conversation.NewStore(record.StorePath, nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close() error = %v", err)
		}
	}()

	snapshot := store.Snapshot(conversation.SnapshotOptions{RecentMessages: 10, RecentExtractions: 10})
	if len(snapshot.Conversations) != 1 {
		t.Fatalf("len(snapshot.Conversations) = %d, want 1", len(snapshot.Conversations))
	}
	if snapshot.Conversations[0].MessageCount != 4 {
		t.Fatalf("MessageCount = %d, want 4", snapshot.Conversations[0].MessageCount)
	}
}

func TestServiceRunTranscriptFailureMarksFailed(t *testing.T) {
	t.Parallel()

	server := newFakeOllamaServer(t, fakeOllamaConfig{failAfterRequests: 4})
	t.Cleanup(server.Close)
	service := newTestService(t, server.URL)

	transcript, err := service.SaveTranscript(Transcript{
		Name: "Failure Case",
		Steps: []TranscriptStep{
			{Message: "Remember this."},
			{Message: "What did I say?"},
		},
	})
	if err != nil {
		t.Fatalf("SaveTranscript() error = %v", err)
	}

	record, err := service.RunTranscript(context.Background(), RunRequest{
		TranscriptID: transcript.ID,
	})
	if err != nil {
		t.Fatalf("RunTranscript() error = %v", err)
	}
	if record.Status != "failed" {
		t.Fatalf("record.Status = %q, want failed", record.Status)
	}
	if record.Error == "" {
		t.Fatalf("expected run error to be recorded")
	}
}

func TestServiceListModelsUsesOllamaCommand(t *testing.T) {
	t.Parallel()

	server := newFakeOllamaServer(t, fakeOllamaConfig{})
	t.Cleanup(server.Close)
	service := newTestService(t, server.URL)
	service.cfg.OllamaCommand = []string{newFakeOllamaCommand(t)}

	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].Name != "llama3.2:3b" || models[1].Name != "qwen2.5:1.5b-instruct" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

type fakeOllamaConfig struct {
	failAfterRequests int
}

func newTestService(t *testing.T, baseURL string) *Service {
	t.Helper()

	service, err := NewService(ServiceConfig{
		BaseDir:             filepath.Join(t.TempDir(), "tests"),
		OllamaBaseURL:       baseURL,
		RequestTimeout:      2 * time.Second,
		DefaultChatModel:    "chat-model",
		DefaultExtractModel: "extract-model",
		DefaultPersona:      "You are testing memory.",
		Telemetry: telemetry.Config{
			Enabled:                true,
			BaseDir:                filepath.Join(t.TempDir(), "telemetry"),
			EnableDiscordReporting: false,
			BufferSize:             32,
			MaxAttachmentBytes:     1024,
			WriteRawPromptFiles:    true,
			WriteRawResponseFiles:  true,
			WriteStoreEvents:       true,
			WriteRuntimeEvents:     true,
		},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func newFakeOllamaCommand(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ollama")
	script := "#!/bin/sh\nif [ \"$1\" != \"list\" ]; then\n  echo \"unexpected args: $@\" >&2\n  exit 1\nfi\ncat <<'EOF'\nNAME                    ID              SIZE      MODIFIED\nqwen2.5:1.5b-instruct   abc123          1.0 GB    2 hours ago\nllama3.2:3b             def456          2.0 GB    3 hours ago\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile(fake ollama) error = %v", err)
	}
	return path
}

func newFakeOllamaServer(t *testing.T, cfg fakeOllamaConfig) *httptest.Server {
	t.Helper()

	requestCount := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}

		requestCount++
		if cfg.failAfterRequests > 0 && requestCount >= cfg.failAfterRequests {
			http.Error(w, "forced failure", http.StatusInternalServerError)
			return
		}

		var payload ollama.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		systemPrompt := ""
		if len(payload.Messages) > 0 {
			systemPrompt = payload.Messages[0].Content
		}

		content := `{"summary":"fallback"}`
		switch {
		case strings.Contains(systemPrompt, "You extract lightweight conversational claims."):
			content = `{"claims":[{"subject":"user","predicate":"name_is","object":"Sam"}]}`
		case strings.Contains(systemPrompt, "You extract active conversational topics."):
			content = `{"topics":["name recall"]}`
		case strings.Contains(systemPrompt, "You summarize a single message for short-term conversational memory."):
			content = `{"summary":"The user is testing whether the assistant remembers a name."}`
		case strings.Contains(systemPrompt, "You update a rolling summary of the active conversation."):
			content = `{"rolling_summary":"The conversation is testing whether the assistant remembers that the user is named Sam."}`
		default:
			content = "You told me your name is Sam."
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{
				"role":    "assistant",
				"content": content,
			},
		})
	}))
}
