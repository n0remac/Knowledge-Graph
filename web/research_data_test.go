package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/claimextract"
	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/researchtest"
)

func TestResearchDataAndRunHandlers(t *testing.T) {
	t.Parallel()

	service := newWebResearchService(t)

	dataRequest := httptest.NewRequest(http.MethodGet, "/research/data", nil)
	dataRecorder := httptest.NewRecorder()
	ResearchDataHandler(service, nil).ServeHTTP(dataRecorder, dataRequest)
	if dataRecorder.Code != http.StatusOK {
		t.Fatalf("GET /research/data status = %d, want 200 body=%s", dataRecorder.Code, dataRecorder.Body.String())
	}

	var data ResearchData
	if err := json.Unmarshal(dataRecorder.Body.Bytes(), &data); err != nil {
		t.Fatalf("json.Unmarshal(data) error = %v", err)
	}
	if data.Defaults.EmbeddingModel == "" {
		t.Fatalf("defaults not populated: %#v", data.Defaults)
	}

	runBody, err := json.Marshal(researchtest.RunRequest{
		Query: "find alpha",
		TopK:  2,
	})
	if err != nil {
		t.Fatalf("json.Marshal(run request) error = %v", err)
	}
	runRequest := httptest.NewRequest(http.MethodPost, "/research/run", bytes.NewReader(runBody))
	runRecorder := httptest.NewRecorder()
	ResearchRunHandler(service, nil).ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("POST /research/run status = %d, want 200 body=%s", runRecorder.Code, runRecorder.Body.String())
	}

	var detail researchtest.RunDetail
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("json.Unmarshal(run detail) error = %v", err)
	}
	if detail.Run.Status != "completed" {
		t.Fatalf("run.Status = %q, want completed (error=%q)", detail.Run.Status, detail.Run.Error)
	}
	if len(detail.Artifacts) == 0 {
		t.Fatalf("expected artifacts in run detail: %#v", detail)
	}

	resourceRequest := httptest.NewRequest(http.MethodGet, "/research/runs/"+detail.Run.ID, nil)
	resourceRecorder := httptest.NewRecorder()
	ResearchRunResourceHandler(service, nil).ServeHTTP(resourceRecorder, resourceRequest)
	if resourceRecorder.Code != http.StatusOK {
		t.Fatalf("GET /research/runs/{id} status = %d, want 200 body=%s", resourceRecorder.Code, resourceRecorder.Body.String())
	}
}

func TestResearchHandlersReturnUnavailableWhenDisabled(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/research/data", nil)
	ResearchDataHandler(nil, errors.New("research slice disabled")).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /research/data status = %d, want 503", recorder.Code)
	}
}

func TestResearchPageEmitsRawScript(t *testing.T) {
	t.Parallel()

	rendered := ResearchPage().Render()
	if !strings.Contains(rendered, "fetch('/research/data'") {
		t.Fatalf("expected raw fetch call in rendered page, got: %s", rendered)
	}
	if strings.Contains(rendered, "fetch(&#39;/research/data&#39;") {
		t.Fatalf("research page script was HTML-escaped: %s", rendered)
	}
	if !strings.Contains(rendered, `data-theme="dark"`) {
		t.Fatalf("expected dark theme default in rendered page, got: %s", rendered)
	}
}

func newWebResearchService(t *testing.T) *researchtest.Service {
	t.Helper()

	ollamaServer := newFakeWebEmbeddingOllamaServer(t)
	t.Cleanup(ollamaServer.Close)
	qdrant := newFakeWebQdrantServer(t)
	t.Cleanup(qdrant.server.Close)

	memoryPath := filepath.Join(t.TempDir(), "memory.db")
	store, err := memory.NewStore(memoryPath, nil)
	if err != nil {
		t.Fatalf("memory.NewStore() error = %v", err)
	}
	index, err := embedding.New(ollama.NewClient(ollamaServer.URL, 2*time.Second), embedding.Config{
		QdrantBaseURL:    qdrant.server.URL,
		CollectionPrefix: "memory-v1",
		RequestTimeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("embedding.New() error = %v", err)
	}
	collector := memory.NewCollector(
		store,
		webResearchClaimsExtractor{result: claimextract.Result{
			Status: "ok",
			Model:  "extract-model",
			Raw:    `{"claims":[]}`,
		}},
		index,
		"nomic-embed-text",
		nil,
	)
	if err := collector.IngestMessage(context.Background(), models.RawMessage{
		MessageID:       "message-1",
		ConversationID:  "channel-1",
		SequenceNumber:  1,
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "alpha",
		TimestampUnixMs: 1000,
	}, memory.CollectOptions{}); err != nil {
		t.Fatalf("collector.IngestMessage() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	service, err := researchtest.NewService(researchtest.ServiceConfig{
		BaseDir:                filepath.Join(t.TempDir(), "research-tests"),
		MemoryStorePath:        memoryPath,
		OllamaBaseURL:          ollamaServer.URL,
		RequestTimeout:         2 * time.Second,
		EmbeddingModel:         "nomic-embed-text",
		QdrantBaseURL:          qdrant.server.URL,
		QdrantCollectionPrefix: "memory-v1",
	})
	if err != nil {
		t.Fatalf("researchtest.NewService() error = %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("service.Close() error = %v", err)
		}
	})
	return service
}

type webResearchClaimsExtractor struct {
	result claimextract.Result
}

func (w webResearchClaimsExtractor) Extract(context.Context, claimextract.Input) claimextract.Result {
	return w.result
}
