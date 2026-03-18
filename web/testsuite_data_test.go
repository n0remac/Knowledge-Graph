package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
	"github.com/n0remac/Knowledge-Graph/internal/testsuite"
)

func TestTestSuiteDataAndRunHandlers(t *testing.T) {
	t.Parallel()

	service := newWebTestSuiteService(t)
	transcript, err := service.SaveTranscript(testsuite.Transcript{
		Name: "Name Recall",
		Steps: []testsuite.TranscriptStep{
			{Message: "My name is Sam."},
			{Message: "What is my name?"},
		},
	})
	if err != nil {
		t.Fatalf("SaveTranscript() error = %v", err)
	}
	_, err = service.SaveRunConfig(testsuite.RunConfig{
		Name:         "Saved Config",
		ChatModel:    "qwen2.5:1.5b-instruct",
		ExtractModel: "llama3.2:3b",
		Persona:      "Be helpful.",
	})
	if err != nil {
		t.Fatalf("SaveRunConfig() error = %v", err)
	}

	dataRequest := httptest.NewRequest(http.MethodGet, "/tests/data", nil)
	dataRecorder := httptest.NewRecorder()
	TestSuiteDataHandler(service).ServeHTTP(dataRecorder, dataRequest)
	if dataRecorder.Code != http.StatusOK {
		t.Fatalf("GET /tests/data status = %d, want 200", dataRecorder.Code)
	}

	var data TestSuiteData
	if err := json.Unmarshal(dataRecorder.Body.Bytes(), &data); err != nil {
		t.Fatalf("json.Unmarshal(data) error = %v", err)
	}
	if len(data.Transcripts) != 1 {
		t.Fatalf("len(data.Transcripts) = %d, want 1", len(data.Transcripts))
	}
	if data.Defaults.ChatModel == "" || data.Defaults.ExtractModel == "" || data.Defaults.Persona == "" {
		t.Fatalf("defaults not populated: %#v", data.Defaults)
	}
	if len(data.Models) != 2 {
		t.Fatalf("len(data.Models) = %d, want 2", len(data.Models))
	}
	if len(data.Configs) != 1 {
		t.Fatalf("len(data.Configs) = %d, want 1", len(data.Configs))
	}

	runBody, err := json.Marshal(testsuite.RunRequest{TranscriptID: transcript.ID})
	if err != nil {
		t.Fatalf("json.Marshal(run request) error = %v", err)
	}
	runRequest := httptest.NewRequest(http.MethodPost, "/tests/run", bytes.NewReader(runBody))
	runRecorder := httptest.NewRecorder()
	TestSuiteRunHandler(service).ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("POST /tests/run status = %d, want 200 body=%s", runRecorder.Code, runRecorder.Body.String())
	}

	var run testsuite.RunRecord
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &run); err != nil {
		t.Fatalf("json.Unmarshal(run) error = %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("run.Status = %q, want completed (error=%q)", run.Status, run.Error)
	}

	resourceRequest := httptest.NewRequest(http.MethodGet, "/tests/runs/"+run.ID+"/conversation", nil)
	resourceRecorder := httptest.NewRecorder()
	TestSuiteRunResourceHandler(service).ServeHTTP(resourceRecorder, resourceRequest)
	if resourceRecorder.Code != http.StatusOK {
		t.Fatalf("GET /tests/runs/{id}/conversation status = %d, want 200 body=%s", resourceRecorder.Code, resourceRecorder.Body.String())
	}

	var conversationData ConversationData
	if err := json.Unmarshal(resourceRecorder.Body.Bytes(), &conversationData); err != nil {
		t.Fatalf("json.Unmarshal(conversationData) error = %v", err)
	}
	if len(conversationData.Conversations) != 1 {
		t.Fatalf("len(conversationData.Conversations) = %d, want 1", len(conversationData.Conversations))
	}
	if conversationData.Conversations[0].MessageCount != 4 {
		t.Fatalf("conversation message count = %d, want 4", conversationData.Conversations[0].MessageCount)
	}
}

func TestTestSuitePageEmitsRawScript(t *testing.T) {
	t.Parallel()

	rendered := TestSuitePage().Render()
	if !strings.Contains(rendered, "fetch('/tests/data'") {
		t.Fatalf("expected raw fetch call in rendered page, got: %s", rendered)
	}
	if strings.Contains(rendered, "fetch(&#39;/tests/data&#39;") {
		t.Fatalf("test suite page script was HTML-escaped: %s", rendered)
	}
}

func TestTestSuiteRunConfigHandlers(t *testing.T) {
	t.Parallel()

	service := newWebTestSuiteService(t)

	body, err := json.Marshal(testsuite.RunConfig{
		Name:         "Reusable Config",
		ChatModel:    "qwen2.5:1.5b-instruct",
		ExtractModel: "llama3.2:3b",
		Persona:      "Answer briefly.",
	})
	if err != nil {
		t.Fatalf("json.Marshal(config) error = %v", err)
	}

	saveRequest := httptest.NewRequest(http.MethodPost, "/tests/configs", bytes.NewReader(body))
	saveRecorder := httptest.NewRecorder()
	TestSuiteRunConfigHandler(service).ServeHTTP(saveRecorder, saveRequest)
	if saveRecorder.Code != http.StatusOK {
		t.Fatalf("POST /tests/configs status = %d, want 200 body=%s", saveRecorder.Code, saveRecorder.Body.String())
	}

	var saved testsuite.RunConfig
	if err := json.Unmarshal(saveRecorder.Body.Bytes(), &saved); err != nil {
		t.Fatalf("json.Unmarshal(saved config) error = %v", err)
	}
	if saved.ID == "" {
		t.Fatalf("saved.ID is empty")
	}

	deleteBody, err := json.Marshal(map[string]string{"id": saved.ID})
	if err != nil {
		t.Fatalf("json.Marshal(delete request) error = %v", err)
	}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/tests/configs/delete", bytes.NewReader(deleteBody))
	deleteRecorder := httptest.NewRecorder()
	TestSuiteRunConfigDeleteHandler(service).ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("POST /tests/configs/delete status = %d, want 200 body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func newWebTestSuiteService(t *testing.T) *testsuite.Service {
	t.Helper()

	server := newFakeWebOllamaServer(t)
	t.Cleanup(server.Close)
	service, err := testsuite.NewService(testsuite.ServiceConfig{
		BaseDir:             filepath.Join(t.TempDir(), "tests"),
		OllamaBaseURL:       server.URL,
		OllamaCommand:       []string{newFakeWebOllamaCommand(t)},
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

func newFakeWebOllamaCommand(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ollama")
	script := "#!/bin/sh\nif [ \"$1\" != \"list\" ]; then\n  echo \"unexpected args: $@\" >&2\n  exit 1\nfi\ncat <<'EOF'\nNAME                    ID              SIZE      MODIFIED\nqwen2.5:1.5b-instruct   abc123          1.0 GB    2 hours ago\nllama3.2:3b             def456          2.0 GB    3 hours ago\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile(fake ollama) error = %v", err)
	}
	return path
}

func newFakeWebOllamaServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		systemPrompt := ""
		if len(payload.Messages) > 0 {
			systemPrompt = payload.Messages[0].Content
		}

		content := "You told me your name is Sam."
		switch {
		case strings.Contains(systemPrompt, "You extract lightweight conversational claims."):
			content = `{"claims":[{"subject":"user","predicate":"name_is","object":"Sam"}]}`
		case strings.Contains(systemPrompt, "You extract active conversational topics."):
			content = `{"topics":["name recall"]}`
		case strings.Contains(systemPrompt, "You summarize a single message for short-term conversational memory."):
			content = `{"summary":"The user is asking the assistant to remember the name Sam."}`
		case strings.Contains(systemPrompt, "You update a rolling summary of the active conversation."):
			content = `{"rolling_summary":"The conversation is about remembering the user name Sam."}`
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
