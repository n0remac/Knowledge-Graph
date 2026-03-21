package research

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
	"github.com/n0remac/Knowledge-Graph/internal/models"
)

func TestMemorySourceMessageHitNormalizesToResearchArtifact(t *testing.T) {
	t.Parallel()

	store := newMemorySourceStore(t)
	searcher := &capturingVectorSearcher{
		response: embedding.SearchResponse{
			CollectionName: "memory-v1",
			Results: []embedding.SearchResult{{
				Rank:       1,
				Score:      0.99,
				DocumentID: "message:message-1",
				Text:       "Alpha memory note",
				Payload: map[string]any{
					"kind":              "message",
					"message_id":        "message-1",
					"conversation_id":   "channel-1",
					"author_id":         "user-1",
					"author_role":       "user",
					"timestamp_unix_ms": float64(1234),
				},
			}},
		},
	}
	source := newMemorySourceWithSearcher(store, searcher, "embed-model")

	artifacts, err := source.Search(context.Background(), SearchRequest{Query: " find alpha "})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1", len(artifacts))
	}

	artifact := artifacts[0]
	if artifact.ArtifactID != "memory:message:message-1" {
		t.Fatalf("ArtifactID = %q", artifact.ArtifactID)
	}
	if artifact.SourceName != memorySourceName || artifact.SourceKind != memorySourceKind {
		t.Fatalf("unexpected source identity: %#v", artifact)
	}
	if artifact.Kind != messageArtifactKind {
		t.Fatalf("Kind = %q", artifact.Kind)
	}
	if artifact.Content != "Alpha memory note" {
		t.Fatalf("Content = %q", artifact.Content)
	}
	if artifact.Provenance.SearchRank != 1 || artifact.Provenance.SearchScore != 0.99 {
		t.Fatalf("unexpected provenance: %#v", artifact.Provenance)
	}
}

func TestMemorySourceClaimHitHydratesParentMessage(t *testing.T) {
	t.Parallel()

	store := newMemorySourceStore(t)
	if _, err := store.SaveMessage(context.Background(), models.RawMessage{
		MessageID:       "message-1",
		ConversationID:  "channel-1",
		SequenceNumber:  1,
		AuthorID:        "user-42",
		AuthorRole:      "user",
		Content:         "Original message",
		TimestampUnixMs: 9876,
	}); err != nil {
		t.Fatalf("SaveMessage() error = %v", err)
	}

	searcher := &capturingVectorSearcher{
		response: embedding.SearchResponse{
			CollectionName: "memory-v1",
			Results: []embedding.SearchResult{{
				Rank:       1,
				Score:      0.88,
				DocumentID: "claim:claim-1",
				Text:       "alpha | stores | memory",
				Payload: map[string]any{
					"kind":            "claim",
					"claim_id":        "claim-1",
					"message_id":      "message-1",
					"conversation_id": "wrong-channel",
				},
			}},
		},
	}
	source := newMemorySourceWithSearcher(store, searcher, "embed-model")

	artifacts, err := source.Search(context.Background(), SearchRequest{Query: "find alpha"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1", len(artifacts))
	}

	artifact := artifacts[0]
	if artifact.Kind != claimArtifactKind {
		t.Fatalf("Kind = %q", artifact.Kind)
	}
	if artifact.AuthorID != "user-42" {
		t.Fatalf("AuthorID = %q, want hydrated parent author", artifact.AuthorID)
	}
	if artifact.ConversationID != "channel-1" {
		t.Fatalf("ConversationID = %q, want hydrated parent conversation", artifact.ConversationID)
	}
	if artifact.TimestampUnixMs != 9876 {
		t.Fatalf("TimestampUnixMs = %d, want 9876", artifact.TimestampUnixMs)
	}
}

func TestMemorySourcePassesConversationFilterOnlyWhenProvided(t *testing.T) {
	t.Parallel()

	store := newMemorySourceStore(t)
	searcher := &capturingVectorSearcher{}
	source := newMemorySourceWithSearcher(store, searcher, "embed-model")

	if _, err := source.Search(context.Background(), SearchRequest{Query: "find alpha"}); err != nil {
		t.Fatalf("Search(global) error = %v", err)
	}
	if searcher.lastFilters != nil {
		t.Fatalf("lastFilters = %#v, want nil for global search", searcher.lastFilters)
	}

	if _, err := source.Search(context.Background(), SearchRequest{Query: "find alpha", ConversationID: "channel-1"}); err != nil {
		t.Fatalf("Search(scoped) error = %v", err)
	}
	if searcher.lastFilters["conversation_id"] != "channel-1" {
		t.Fatalf("conversation filter = %#v", searcher.lastFilters)
	}
}

func TestMemorySourceDeduplicatesArtifactsByBestScore(t *testing.T) {
	t.Parallel()

	store := newMemorySourceStore(t)
	searcher := &capturingVectorSearcher{
		response: embedding.SearchResponse{
			CollectionName: "memory-v1",
			Results: []embedding.SearchResult{
				{
					Rank:       2,
					Score:      0.60,
					DocumentID: "message:message-1",
					Text:       "Alpha memory note",
					Payload: map[string]any{
						"kind":              "message",
						"message_id":        "message-1",
						"conversation_id":   "channel-1",
						"author_id":         "user-1",
						"timestamp_unix_ms": float64(1234),
					},
				},
				{
					Rank:       1,
					Score:      0.95,
					DocumentID: "message:message-1",
					Text:       "Alpha memory note",
					Payload: map[string]any{
						"kind":              "message",
						"message_id":        "message-1",
						"conversation_id":   "channel-1",
						"author_id":         "user-1",
						"timestamp_unix_ms": float64(1234),
					},
				},
			},
		},
	}
	source := newMemorySourceWithSearcher(store, searcher, "embed-model")

	artifacts, err := source.Search(context.Background(), SearchRequest{Query: "find alpha"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1", len(artifacts))
	}
	if artifacts[0].Provenance.SearchScore != 0.95 {
		t.Fatalf("SearchScore = %f, want 0.95", artifacts[0].Provenance.SearchScore)
	}
	if artifacts[0].Provenance.SearchRank != 1 {
		t.Fatalf("SearchRank = %d, want 1", artifacts[0].Provenance.SearchRank)
	}
}

func newMemorySourceStore(t *testing.T) *memory.Store {
	t.Helper()

	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memory.db"), nil)
	if err != nil {
		t.Fatalf("memory.NewStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close() error = %v", err)
		}
	})
	return store
}

type capturingVectorSearcher struct {
	response    embedding.SearchResponse
	lastFilters map[string]string
	err         error
}

func (c *capturingVectorSearcher) Search(_ context.Context, _, _ string, _ int, filters map[string]string) (embedding.SearchResponse, error) {
	if filters != nil {
		c.lastFilters = make(map[string]string, len(filters))
		for key, value := range filters {
			c.lastFilters[key] = value
		}
	} else {
		c.lastFilters = nil
	}
	return c.response, c.err
}
