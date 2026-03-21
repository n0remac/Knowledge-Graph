package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/embeddingtest"
)

func TestEmbeddingDataAndRunHandlers(t *testing.T) {
	t.Parallel()

	service := newWebEmbeddingService(t)
	set, err := service.SaveMessageSet(embeddingtest.MessageSet{
		Name: "Recall Facts",
		Messages: []embeddingtest.MessageEntry{
			{Text: "alpha"},
			{Text: "beta"},
		},
	})
	if err != nil {
		t.Fatalf("SaveMessageSet() error = %v", err)
	}

	dataRequest := httptest.NewRequest(http.MethodGet, "/embeddings/data", nil)
	dataRecorder := httptest.NewRecorder()
	EmbeddingDataHandler(service, nil).ServeHTTP(dataRecorder, dataRequest)
	if dataRecorder.Code != http.StatusOK {
		t.Fatalf("GET /embeddings/data status = %d, want 200", dataRecorder.Code)
	}

	var data EmbeddingData
	if err := json.Unmarshal(dataRecorder.Body.Bytes(), &data); err != nil {
		t.Fatalf("json.Unmarshal(data) error = %v", err)
	}
	if data.Defaults.EmbeddingModel == "" {
		t.Fatalf("defaults not populated: %#v", data.Defaults)
	}
	if len(data.Models) != 2 {
		t.Fatalf("len(data.Models) = %d, want 2", len(data.Models))
	}
	if len(data.MessageSets) != 1 {
		t.Fatalf("len(data.MessageSets) = %d, want 1", len(data.MessageSets))
	}

	runBody, err := json.Marshal(embeddingtest.RunRequest{
		MessageSetID: set.ID,
		Query:        "find alpha",
		TopK:         2,
	})
	if err != nil {
		t.Fatalf("json.Marshal(run request) error = %v", err)
	}
	runRequest := httptest.NewRequest(http.MethodPost, "/embeddings/run", bytes.NewReader(runBody))
	runRecorder := httptest.NewRecorder()
	EmbeddingRunHandler(service, nil).ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("POST /embeddings/run status = %d, want 200 body=%s", runRecorder.Code, runRecorder.Body.String())
	}

	var run embeddingtest.RunRecord
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &run); err != nil {
		t.Fatalf("json.Unmarshal(run) error = %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("run.Status = %q, want completed (error=%q)", run.Status, run.Error)
	}

	resourceRequest := httptest.NewRequest(http.MethodGet, "/embeddings/runs/"+run.ID, nil)
	resourceRecorder := httptest.NewRecorder()
	EmbeddingRunResourceHandler(service, nil).ServeHTTP(resourceRecorder, resourceRequest)
	if resourceRecorder.Code != http.StatusOK {
		t.Fatalf("GET /embeddings/runs/{id} status = %d, want 200 body=%s", resourceRecorder.Code, resourceRecorder.Body.String())
	}
}

func TestEmbeddingMessageSetHandlers(t *testing.T) {
	t.Parallel()

	service := newWebEmbeddingService(t)

	body, err := json.Marshal(embeddingtest.MessageSet{
		Name: "Reusable Set",
		Messages: []embeddingtest.MessageEntry{
			{Text: "alpha"},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal(set) error = %v", err)
	}

	saveRequest := httptest.NewRequest(http.MethodPost, "/embeddings/message-sets", bytes.NewReader(body))
	saveRecorder := httptest.NewRecorder()
	EmbeddingMessageSetHandler(service, nil).ServeHTTP(saveRecorder, saveRequest)
	if saveRecorder.Code != http.StatusOK {
		t.Fatalf("POST /embeddings/message-sets status = %d, want 200 body=%s", saveRecorder.Code, saveRecorder.Body.String())
	}

	var saved embeddingtest.MessageSet
	if err := json.Unmarshal(saveRecorder.Body.Bytes(), &saved); err != nil {
		t.Fatalf("json.Unmarshal(saved) error = %v", err)
	}
	if saved.ID == "" {
		t.Fatalf("saved.ID is empty")
	}

	deleteBody, err := json.Marshal(map[string]string{"id": saved.ID})
	if err != nil {
		t.Fatalf("json.Marshal(delete request) error = %v", err)
	}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/embeddings/message-sets/delete", bytes.NewReader(deleteBody))
	deleteRecorder := httptest.NewRecorder()
	EmbeddingMessageSetDeleteHandler(service, nil).ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("POST /embeddings/message-sets/delete status = %d, want 200 body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func TestEmbeddingHandlersReturnUnavailableWhenDisabled(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/embeddings/data", nil)
	EmbeddingDataHandler(nil, errors.New("embedding slice disabled")).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /embeddings/data status = %d, want 503", recorder.Code)
	}
}

func TestEmbeddingPageEmitsRawScript(t *testing.T) {
	t.Parallel()

	rendered := EmbeddingPage().Render()
	if !strings.Contains(rendered, "fetch('/embeddings/data'") {
		t.Fatalf("expected raw fetch call in rendered page, got: %s", rendered)
	}
	if strings.Contains(rendered, "fetch(&#39;/embeddings/data&#39;") {
		t.Fatalf("embedding page script was HTML-escaped: %s", rendered)
	}
	if !strings.Contains(rendered, `data-theme="dark"`) {
		t.Fatalf("expected dark theme default in rendered page, got: %s", rendered)
	}
}

func newWebEmbeddingService(t *testing.T) *embeddingtest.Service {
	t.Helper()

	ollamaServer := newFakeWebEmbeddingOllamaServer(t)
	t.Cleanup(ollamaServer.Close)
	qdrant := newFakeWebQdrantServer(t)
	t.Cleanup(qdrant.server.Close)

	service, err := embeddingtest.NewService(embeddingtest.ServiceConfig{
		BaseDir:                filepath.Join(t.TempDir(), "embedding-tests"),
		OllamaBaseURL:          ollamaServer.URL,
		OllamaCommand:          []string{newFakeWebEmbeddingOllamaCommand(t)},
		RequestTimeout:         2 * time.Second,
		DefaultEmbeddingModel:  "nomic-embed-text",
		QdrantBaseURL:          qdrant.server.URL,
		QdrantCollectionPrefix: "embedding-v1",
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func newFakeWebEmbeddingOllamaCommand(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ollama")
	script := "#!/bin/sh\nif [ \"$1\" != \"list\" ]; then\n  echo \"unexpected args: $@\" >&2\n  exit 1\nfi\ncat <<'EOF'\nNAME                    ID              SIZE      MODIFIED\nnomic-embed-text        abc123          1.0 GB    2 hours ago\nmxbai-embed-large       def456          2.0 GB    3 hours ago\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile(fake ollama) error = %v", err)
	}
	return path
}

func newFakeWebEmbeddingOllamaServer(t *testing.T) *httptest.Server {
	t.Helper()

	vectors := map[string][]float64{
		"alpha":      {1, 0, 0},
		"beta":       {0.7, 0.3, 0},
		"find alpha": {1, 0, 0},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		vector, ok := vectors[payload.Input]
		if !ok {
			http.Error(w, "unknown input", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float64{vector},
		})
	}))
}

type fakeWebQdrantServer struct {
	server      *httptest.Server
	mu          sync.Mutex
	collections map[string]*fakeWebCollection
}

type fakeWebCollection struct {
	size   int
	points map[uint64]fakeWebPoint
}

type fakeWebPoint struct {
	Vector  []float64
	Payload map[string]any
}

func newFakeWebQdrantServer(t *testing.T) *fakeWebQdrantServer {
	t.Helper()

	state := &fakeWebQdrantServer{
		collections: make(map[string]*fakeWebCollection),
	}
	state.server = httptest.NewServer(http.HandlerFunc(state.handle))
	return state
}

func (s *fakeWebQdrantServer) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/collections/")
	if path == r.URL.Path {
		http.NotFound(w, r)
		return
	}

	switch {
	case strings.HasSuffix(path, "/points/search"):
		s.handleSearch(w, r, strings.TrimSuffix(path, "/points/search"))
	case strings.HasSuffix(path, "/points"):
		s.handleUpsert(w, r, strings.TrimSuffix(path, "/points"))
	default:
		switch r.Method {
		case http.MethodGet:
			s.handleGetCollection(w, strings.Trim(path, "/"))
		case http.MethodPut:
			s.handleCreateCollection(w, r, strings.Trim(path, "/"))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func (s *fakeWebQdrantServer) handleGetCollection(w http.ResponseWriter, name string) {
	s.mu.Lock()
	collection, ok := s.collections[name]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"result": map[string]any{
			"config": map[string]any{
				"params": map[string]any{
					"vectors": map[string]any{
						"size": collection.size,
					},
				},
			},
		},
	})
}

func (s *fakeWebQdrantServer) handleCreateCollection(w http.ResponseWriter, r *http.Request, name string) {
	var payload struct {
		Vectors struct {
			Size int `json:"size"`
		} `json:"vectors"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.collections[name] = &fakeWebCollection{
		size:   payload.Vectors.Size,
		points: make(map[uint64]fakeWebPoint),
	}
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

func (s *fakeWebQdrantServer) handleUpsert(w http.ResponseWriter, r *http.Request, name string) {
	var payload struct {
		Points []struct {
			ID      uint64         `json:"id"`
			Vector  []float64      `json:"vector"`
			Payload map[string]any `json:"payload"`
		} `json:"points"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	collection, ok := s.collections[name]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	for _, point := range payload.Points {
		collection.points[point.ID] = fakeWebPoint{
			Vector:  append([]float64(nil), point.Vector...),
			Payload: point.Payload,
		}
	}
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

func (s *fakeWebQdrantServer) handleSearch(w http.ResponseWriter, r *http.Request, name string) {
	var payload struct {
		Vector []float64 `json:"vector"`
		Limit  int       `json:"limit"`
		Filter struct {
			Must []struct {
				Key   string `json:"key"`
				Match struct {
					Value string `json:"value"`
				} `json:"match"`
			} `json:"must"`
		} `json:"filter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filters := make(map[string]string, len(payload.Filter.Must))
	for _, match := range payload.Filter.Must {
		filters[match.Key] = match.Match.Value
	}

	s.mu.Lock()
	collection, ok := s.collections[name]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	type hit struct {
		Score   float64        `json:"score"`
		Payload map[string]any `json:"payload"`
	}
	hits := make([]hit, 0, len(collection.points))
	for _, point := range collection.points {
		if !payloadMatchesWeb(point.Payload, filters) {
			continue
		}
		hits = append(hits, hit{
			Score:   cosineWeb(point.Vector, payload.Vector),
			Payload: point.Payload,
		})
	}
	s.mu.Unlock()

	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})
	if payload.Limit > 0 && len(hits) > payload.Limit {
		hits = hits[:payload.Limit]
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"result": hits})
}

func payloadMatchesWeb(payload map[string]any, filters map[string]string) bool {
	for key, value := range filters {
		if fmt.Sprint(payload[key]) != value {
			return false
		}
	}
	return true
}

func cosineWeb(a, b []float64) float64 {
	var dot float64
	var magA float64
	var magB float64
	for idx := range a {
		if idx >= len(b) {
			break
		}
		dot += a[idx] * b[idx]
		magA += a[idx] * a[idx]
		magB += b[idx] * b[idx]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
