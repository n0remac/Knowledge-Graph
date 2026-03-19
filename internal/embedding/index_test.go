package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/ollama"
)

func TestIndexSearchAndFilter(t *testing.T) {
	t.Parallel()

	ollamaServer := newFakeEmbeddingOllamaServer(t)
	t.Cleanup(ollamaServer.Close)
	qdrant := newFakeQdrantServer(t)
	t.Cleanup(qdrant.server.Close)

	index, err := New(ollama.NewClient(ollamaServer.URL, 2*time.Second), Config{
		QdrantBaseURL:    qdrant.server.URL,
		CollectionPrefix: "embedding-v1",
		RequestTimeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if _, err := index.IndexMessages(ctx, "nomic-embed-text", []Document{
		{MessageSetID: "set-a", MessageIndex: 0, MessageText: "alpha"},
		{MessageSetID: "set-a", MessageIndex: 1, MessageText: "beta"},
		{MessageSetID: "set-b", MessageIndex: 0, MessageText: "gamma"},
	}); err != nil {
		t.Fatalf("IndexMessages() error = %v", err)
	}

	result, err := index.Search(ctx, "nomic-embed-text", "set-a", "find alpha", 2)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.CollectionName != "embedding-v1-nomic-embed-text" {
		t.Fatalf("CollectionName = %q", result.CollectionName)
	}
	if len(result.Results) != 2 {
		t.Fatalf("len(result.Results) = %d, want 2", len(result.Results))
	}
	if result.Results[0].MessageText != "alpha" {
		t.Fatalf("first result = %#v, want alpha first", result.Results[0])
	}
	for _, item := range result.Results {
		if item.MessageText == "gamma" {
			t.Fatalf("Search() returned message from a different message set: %#v", item)
		}
	}
}

func TestIndexMessagesUpsertsDeterministically(t *testing.T) {
	t.Parallel()

	ollamaServer := newFakeEmbeddingOllamaServer(t)
	t.Cleanup(ollamaServer.Close)
	qdrant := newFakeQdrantServer(t)
	t.Cleanup(qdrant.server.Close)

	index, err := New(ollama.NewClient(ollamaServer.URL, 2*time.Second), Config{
		QdrantBaseURL:    qdrant.server.URL,
		CollectionPrefix: "embedding-v1",
		RequestTimeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	docs := []Document{
		{MessageSetID: "set-a", MessageIndex: 0, MessageText: "alpha"},
		{MessageSetID: "set-a", MessageIndex: 1, MessageText: "beta"},
	}
	if _, err := index.IndexMessages(ctx, "nomic-embed-text", docs); err != nil {
		t.Fatalf("IndexMessages(first) error = %v", err)
	}
	if _, err := index.IndexMessages(ctx, "nomic-embed-text", []Document{
		{MessageSetID: "set-a", MessageIndex: 0, MessageText: "alpha revised"},
		{MessageSetID: "set-a", MessageIndex: 1, MessageText: "beta"},
	}); err != nil {
		t.Fatalf("IndexMessages(second) error = %v", err)
	}

	collection := qdrant.collection("embedding-v1-nomic-embed-text")
	if len(collection.points) != 2 {
		t.Fatalf("len(points) = %d, want 2 after deterministic upsert", len(collection.points))
	}

	result, err := index.Search(ctx, "nomic-embed-text", "set-a", "find alpha", 2)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.Results[0].MessageText != "alpha revised" {
		t.Fatalf("first result = %#v, want revised payload", result.Results[0])
	}
}

func TestIndexMessagesRejectsIncompatibleCollection(t *testing.T) {
	t.Parallel()

	ollamaServer := newFakeEmbeddingOllamaServer(t)
	t.Cleanup(ollamaServer.Close)
	qdrant := newFakeQdrantServer(t)
	t.Cleanup(qdrant.server.Close)
	qdrant.collections["embedding-v1-nomic-embed-text"] = &fakeCollection{
		size:   2,
		points: make(map[uint64]fakePoint),
	}

	index, err := New(ollama.NewClient(ollamaServer.URL, 2*time.Second), Config{
		QdrantBaseURL:    qdrant.server.URL,
		CollectionPrefix: "embedding-v1",
		RequestTimeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := index.IndexMessages(context.Background(), "nomic-embed-text", []Document{
		{MessageSetID: "set-a", MessageIndex: 0, MessageText: "alpha"},
	}); err == nil || !strings.Contains(err.Error(), "vector size") {
		t.Fatalf("IndexMessages() error = %v, want vector size mismatch", err)
	}
}

func newFakeEmbeddingOllamaServer(t *testing.T) *httptest.Server {
	t.Helper()

	vectors := map[string][]float64{
		"alpha":         {1, 0, 0},
		"alpha revised": {0.95, 0.05, 0},
		"beta":          {0.7, 0.3, 0},
		"gamma":         {0, 1, 0},
		"find alpha":    {1, 0, 0},
		"find gamma":    {0, 1, 0},
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

func (s *fakeQdrantServer) collection(name string) *fakeCollection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.collections[name]
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
		http.NotFound(w, r)
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

	messageSetID := ""
	if len(payload.Filter.Must) > 0 {
		messageSetID = payload.Filter.Must[0].Match.Value
	}

	s.mu.Lock()
	collection, ok := s.collections[name]
	if !ok {
		s.mu.Unlock()
		http.NotFound(w, r)
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
