package embeddingtest

import (
	"context"
	"encoding/json"
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
)

func TestServiceMessageSetCRUD(t *testing.T) {
	t.Parallel()

	service := newTestEmbeddingService(t)
	created, err := service.SaveMessageSet(MessageSet{
		Name:        "Recall Facts",
		Description: "Simple retrieval set.",
		Messages: []MessageEntry{
			{Text: "alpha"},
			{Text: "beta"},
		},
	})
	if err != nil {
		t.Fatalf("SaveMessageSet(create) error = %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created.ID is empty")
	}

	sets, err := service.ListMessageSets()
	if err != nil {
		t.Fatalf("ListMessageSets() error = %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("len(sets) = %d, want 1", len(sets))
	}

	created.Description = "Updated description."
	updated, err := service.SaveMessageSet(created)
	if err != nil {
		t.Fatalf("SaveMessageSet(update) error = %v", err)
	}
	if updated.Description != "Updated description." {
		t.Fatalf("updated.Description = %q", updated.Description)
	}

	if err := service.DeleteMessageSet(created.ID); err != nil {
		t.Fatalf("DeleteMessageSet() error = %v", err)
	}
	if _, err := service.GetMessageSet(created.ID); err == nil {
		t.Fatalf("GetMessageSet() after delete returned nil error")
	}
}

func TestServiceRunQueryPersistsResults(t *testing.T) {
	t.Parallel()

	service := newTestEmbeddingService(t)
	set, err := service.SaveMessageSet(MessageSet{
		Name: "Recall Facts",
		Messages: []MessageEntry{
			{Text: "alpha"},
			{Text: "beta"},
			{Text: "gamma"},
		},
	})
	if err != nil {
		t.Fatalf("SaveMessageSet() error = %v", err)
	}

	record, err := service.RunQuery(context.Background(), RunRequest{
		MessageSetID: set.ID,
		Query:        "find alpha",
		TopK:         2,
	})
	if err != nil {
		t.Fatalf("RunQuery() error = %v", err)
	}
	if record.Status != "completed" {
		t.Fatalf("record.Status = %q, want completed (error=%q)", record.Status, record.Error)
	}
	if len(record.Results) != 2 {
		t.Fatalf("len(record.Results) = %d, want 2", len(record.Results))
	}
	if record.Results[0].MessageText != "alpha" {
		t.Fatalf("first result = %#v, want alpha first", record.Results[0])
	}

	saved, err := service.GetRun(record.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if saved.Status != "completed" {
		t.Fatalf("saved.Status = %q, want completed", saved.Status)
	}
	if _, err := os.Stat(filepath.Join(service.cfg.BaseDir, "runs", record.ID, "result.json")); err != nil {
		t.Fatalf("expected persisted run record: %v", err)
	}
}

func TestServiceRunQueryRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	service := newTestEmbeddingService(t)
	if _, err := service.SaveMessageSet(MessageSet{
		Name:     "Invalid",
		Messages: []MessageEntry{{Text: " "}},
	}); err == nil {
		t.Fatal("SaveMessageSet() error = nil, want empty message error")
	}

	set, err := service.SaveMessageSet(MessageSet{
		Name:     "Valid",
		Messages: []MessageEntry{{Text: "alpha"}},
	})
	if err != nil {
		t.Fatalf("SaveMessageSet(valid) error = %v", err)
	}

	if _, err := service.RunQuery(context.Background(), RunRequest{
		MessageSetID: set.ID,
		Query:        " ",
	}); err == nil {
		t.Fatal("RunQuery() error = nil, want empty query error")
	}

	if _, err := service.RunQuery(context.Background(), RunRequest{
		MessageSetID: set.ID,
		Query:        "find alpha",
		TopK:         99,
	}); err == nil {
		t.Fatal("RunQuery() error = nil, want top_k bounds error")
	}
}

func TestServiceListModelsUsesOllamaCommand(t *testing.T) {
	t.Parallel()

	service := newTestEmbeddingService(t)
	service.cfg.OllamaCommand = []string{newFakeEmbeddingOllamaCommand(t)}

	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
}

func newTestEmbeddingService(t *testing.T) *Service {
	t.Helper()

	ollamaServer := newFakeOllamaServer(t)
	t.Cleanup(ollamaServer.Close)
	qdrant := newFakeQdrantServer(t)
	t.Cleanup(qdrant.server.Close)

	service, err := NewService(ServiceConfig{
		BaseDir:                filepath.Join(t.TempDir(), "embedding-tests"),
		OllamaBaseURL:          ollamaServer.URL,
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

func newFakeEmbeddingOllamaCommand(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ollama")
	script := "#!/bin/sh\nif [ \"$1\" != \"list\" ]; then\n  echo \"unexpected args: $@\" >&2\n  exit 1\nfi\ncat <<'EOF'\nNAME                    ID              SIZE      MODIFIED\nnomic-embed-text        abc123          1.0 GB    2 hours ago\nmxbai-embed-large       def456          2.0 GB    3 hours ago\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile(fake ollama) error = %v", err)
	}
	return path
}

func newFakeOllamaServer(t *testing.T) *httptest.Server {
	t.Helper()

	vectors := map[string][]float64{
		"alpha":      {1, 0, 0},
		"beta":       {0.7, 0.3, 0},
		"gamma":      {0, 1, 0},
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

type fakeQdrantServer struct {
	server      *httptest.Server
	mu          sync.Mutex
	collections map[string]*fakeCollection
}

type fakeCollection struct {
	size   int
	points map[uint64]fakePoint
}

type fakePoint struct {
	Vector  []float64
	Payload map[string]any
}

func newFakeQdrantServer(t *testing.T) *fakeQdrantServer {
	t.Helper()

	state := &fakeQdrantServer{
		collections: make(map[string]*fakeCollection),
	}
	state.server = httptest.NewServer(http.HandlerFunc(state.handle))
	return state
}

func (s *fakeQdrantServer) handle(w http.ResponseWriter, r *http.Request) {
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

func (s *fakeQdrantServer) handleGetCollection(w http.ResponseWriter, name string) {
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

func (s *fakeQdrantServer) handleCreateCollection(w http.ResponseWriter, r *http.Request, name string) {
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
	s.collections[name] = &fakeCollection{
		size:   payload.Vectors.Size,
		points: make(map[uint64]fakePoint),
	}
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

func (s *fakeQdrantServer) handleUpsert(w http.ResponseWriter, r *http.Request, name string) {
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
		collection.points[point.ID] = fakePoint{
			Vector:  append([]float64(nil), point.Vector...),
			Payload: point.Payload,
		}
	}
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

func (s *fakeQdrantServer) handleSearch(w http.ResponseWriter, r *http.Request, name string) {
	var payload struct {
		Vector []float64 `json:"vector"`
		Limit  int       `json:"limit"`
		Filter struct {
			Must []struct {
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

	messageSetID := ""
	if len(payload.Filter.Must) > 0 {
		messageSetID = payload.Filter.Must[0].Match.Value
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
		if fmt.Sprint(point.Payload["message_set_id"]) != messageSetID {
			continue
		}
		hits = append(hits, hit{
			Score:   cosine(point.Vector, payload.Vector),
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

func cosine(a, b []float64) float64 {
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
