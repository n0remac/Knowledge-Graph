package researchtest

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

	"github.com/n0remac/Knowledge-Graph/internal/claimextract"
	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
)

func TestServiceRunQueryWritesArtifactsAndListsRuns(t *testing.T) {
	t.Parallel()

	service, baseDir := newResearchTestService(t)

	detail, err := service.RunQuery(context.Background(), RunRequest{
		Query: "find alpha",
		TopK:  4,
	})
	if err != nil {
		t.Fatalf("RunQuery() error = %v", err)
	}
	if detail.Run.Status != "completed" {
		t.Fatalf("Run status = %q, want completed (error=%q)", detail.Run.Status, detail.Run.Error)
	}
	if detail.Run.ArtifactCount == 0 || len(detail.Artifacts) == 0 {
		t.Fatalf("expected artifacts in run detail: %#v", detail)
	}
	if detail.Run.Context.RenderedBrief == "" {
		t.Fatalf("RenderedBrief is empty: %#v", detail.Run.Context)
	}

	resultPath := filepath.Join(baseDir, "runs", detail.Run.ID, "result.json")
	if _, err := os.Stat(resultPath); err != nil {
		t.Fatalf("result.json stat error = %v", err)
	}
	artifactsPath := filepath.Join(baseDir, "runs", detail.Run.ID, "artifacts.json")
	if _, err := os.Stat(artifactsPath); err != nil {
		t.Fatalf("artifacts.json stat error = %v", err)
	}

	runs, err := service.ListRuns(10)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) == 0 || runs[0].ID != detail.Run.ID {
		t.Fatalf("runs = %#v, want latest run first", runs)
	}

	loaded, err := service.GetRun(detail.Run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if loaded.Run.ID != detail.Run.ID {
		t.Fatalf("loaded.Run.ID = %q, want %q", loaded.Run.ID, detail.Run.ID)
	}
	if len(loaded.Artifacts) != len(detail.Artifacts) {
		t.Fatalf("len(loaded.Artifacts) = %d, want %d", len(loaded.Artifacts), len(detail.Artifacts))
	}
}

func TestServiceRunQueryRejectsBadRequests(t *testing.T) {
	t.Parallel()

	service, _ := newResearchTestService(t)

	if _, err := service.RunQuery(context.Background(), RunRequest{}); err == nil || !strings.Contains(err.Error(), "query cannot be empty") {
		t.Fatalf("RunQuery(empty) error = %v, want query validation error", err)
	}
	if _, err := service.RunQuery(context.Background(), RunRequest{Query: "find alpha", TopK: 99}); err == nil || !strings.Contains(err.Error(), "top_k must be between") {
		t.Fatalf("RunQuery(topk) error = %v, want top_k validation error", err)
	}
}

func TestNewServiceFailsWhenMemoryStoreIsMissing(t *testing.T) {
	t.Parallel()

	_, err := NewService(ServiceConfig{
		BaseDir:                filepath.Join(t.TempDir(), "research-tests"),
		MemoryStorePath:        filepath.Join(t.TempDir(), "missing-memory.db"),
		OllamaBaseURL:          "http://127.0.0.1:11434",
		RequestTimeout:         2 * time.Second,
		EmbeddingModel:         "nomic-embed-text",
		QdrantBaseURL:          "http://127.0.0.1:6333",
		QdrantCollectionPrefix: "memory-v1",
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("NewService() error = %v, want missing memory store error", err)
	}
}

func TestNewServiceFailsWhenEmbeddingConfigIsInvalid(t *testing.T) {
	t.Parallel()

	memoryPath := filepath.Join(t.TempDir(), "memory.db")
	store, err := memory.NewStore(memoryPath, nil)
	if err != nil {
		t.Fatalf("memory.NewStore() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	_, err = NewService(ServiceConfig{
		BaseDir:                filepath.Join(t.TempDir(), "research-tests"),
		MemoryStorePath:        memoryPath,
		OllamaBaseURL:          "http://127.0.0.1:11434",
		RequestTimeout:         2 * time.Second,
		EmbeddingModel:         "",
		QdrantBaseURL:          "http://127.0.0.1:6333",
		QdrantCollectionPrefix: "memory-v1",
	})
	if err == nil || !strings.Contains(err.Error(), "embedding model cannot be empty") {
		t.Fatalf("NewService() error = %v, want embedding config error", err)
	}
}

func newResearchTestService(t *testing.T) (*Service, string) {
	t.Helper()

	ollamaServer := newFakeResearchOllamaServer(t)
	t.Cleanup(ollamaServer.Close)
	qdrant := newFakeResearchQdrantServer(t)
	t.Cleanup(qdrant.server.Close)

	baseDir := filepath.Join(t.TempDir(), "research-tests")
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
		fakeClaimsExtractor{result: claimextract.Result{
			Claims: []models.Claim{{Subject: "alpha", Predicate: "stores", Object: "memory", SourceMessageID: "message-1"}},
			Status: "ok",
			Model:  "extract-model",
			Raw:    `{"claims":[{"subject":"alpha","predicate":"stores","object":"memory"}]}`,
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
		Content:         "alpha note",
		TimestampUnixMs: 1000,
	}, memory.CollectOptions{}); err != nil {
		t.Fatalf("collector.IngestMessage(message-1) error = %v", err)
	}
	if err := collector.IngestMessage(context.Background(), models.RawMessage{
		MessageID:       "message-2",
		ConversationID:  "channel-2",
		SequenceNumber:  2,
		AuthorID:        "user-2",
		AuthorRole:      "user",
		Content:         "gamma note",
		TimestampUnixMs: 2000,
	}, memory.CollectOptions{}); err != nil {
		t.Fatalf("collector.IngestMessage(message-2) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	service, err := NewService(ServiceConfig{
		BaseDir:                baseDir,
		MemoryStorePath:        memoryPath,
		OllamaBaseURL:          ollamaServer.URL,
		RequestTimeout:         2 * time.Second,
		EmbeddingModel:         "nomic-embed-text",
		QdrantBaseURL:          qdrant.server.URL,
		QdrantCollectionPrefix: "memory-v1",
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("service.Close() error = %v", err)
		}
	})
	return service, baseDir
}

type fakeClaimsExtractor struct {
	result claimextract.Result
}

func (f fakeClaimsExtractor) Extract(context.Context, claimextract.Input) claimextract.Result {
	return f.result
}

func newFakeResearchOllamaServer(t *testing.T) *httptest.Server {
	t.Helper()

	vectors := map[string][]float64{
		"alpha note":              {1, 0, 0},
		"alpha | stores | memory": {0.98, 0.02, 0},
		"gamma note":              {0, 1, 0},
		"find alpha":              {1, 0, 0},
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

type fakeResearchQdrantServer struct {
	server      *httptest.Server
	mu          sync.Mutex
	collections map[string]*fakeResearchCollection
}

type fakeResearchCollection struct {
	size   int
	points map[uint64]fakeResearchPoint
}

type fakeResearchPoint struct {
	Vector  []float64
	Payload map[string]any
}

func newFakeResearchQdrantServer(t *testing.T) *fakeResearchQdrantServer {
	t.Helper()

	state := &fakeResearchQdrantServer{
		collections: make(map[string]*fakeResearchCollection),
	}
	state.server = httptest.NewServer(http.HandlerFunc(state.handle))
	return state
}

func (s *fakeResearchQdrantServer) handle(w http.ResponseWriter, r *http.Request) {
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

func (s *fakeResearchQdrantServer) handleGetCollection(w http.ResponseWriter, name string) {
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

func (s *fakeResearchQdrantServer) handleCreateCollection(w http.ResponseWriter, r *http.Request, name string) {
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
	defer s.mu.Unlock()
	s.collections[name] = &fakeResearchCollection{
		size:   payload.Vectors.Size,
		points: make(map[uint64]fakeResearchPoint),
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
}

func (s *fakeResearchQdrantServer) handleUpsert(w http.ResponseWriter, r *http.Request, name string) {
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
	defer s.mu.Unlock()
	collection, ok := s.collections[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	for _, point := range payload.Points {
		collection.points[point.ID] = fakeResearchPoint{
			Vector:  append([]float64(nil), point.Vector...),
			Payload: clonePayload(point.Payload),
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
}

func (s *fakeResearchQdrantServer) handleSearch(w http.ResponseWriter, r *http.Request, name string) {
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
		http.NotFound(w, r)
		return
	}

	type hit struct {
		Score   float64        `json:"score"`
		Payload map[string]any `json:"payload"`
	}
	hits := make([]hit, 0, len(collection.points))
	for _, point := range collection.points {
		if !payloadMatches(point.Payload, filters) {
			continue
		}
		hits = append(hits, hit{
			Score:   cosine(point.Vector, payload.Vector),
			Payload: clonePayload(point.Payload),
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

func payloadMatches(payload map[string]any, filters map[string]string) bool {
	for key, value := range filters {
		if fmt.Sprint(payload[key]) != value {
			return false
		}
	}
	return true
}

func clonePayload(input map[string]any) map[string]any {
	if len(input) == 0 {
		return make(map[string]any)
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
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
	return dot / math.Sqrt(magA*magB)
}
