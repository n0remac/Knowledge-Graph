package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientEmbedDetailed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, 2*time.Second)
	result, err := client.EmbedDetailed(context.Background(), EmbeddingRequest{
		Model: "nomic-embed-text",
		Input: "hello world",
	})
	if err != nil {
		t.Fatalf("EmbedDetailed() error = %v", err)
	}
	if len(result.Vector) != 3 {
		t.Fatalf("len(result.Vector) = %d, want 3", len(result.Vector))
	}
	if result.Request.Model != "nomic-embed-text" {
		t.Fatalf("result.Request.Model = %q", result.Request.Model)
	}
	if result.RawResponse == "" {
		t.Fatalf("result.RawResponse is empty")
	}
}

func TestClientEmbedDetailedReturnsStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, 2*time.Second)
	if _, err := client.EmbedDetailed(context.Background(), EmbeddingRequest{Model: "embed", Input: "hello"}); err == nil {
		t.Fatal("EmbedDetailed() error = nil, want status error")
	}
}

func TestClientEmbedDetailedRejectsMalformedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":"oops"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, 2*time.Second)
	if _, err := client.EmbedDetailed(context.Background(), EmbeddingRequest{Model: "embed", Input: "hello"}); err == nil {
		t.Fatal("EmbedDetailed() error = nil, want decode error")
	}
}
