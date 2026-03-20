package memory

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/n0remac/Knowledge-Graph/internal/claimextract"
	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/models"
)

func TestCollectorIngestsMessageClaimsAndVectorDocuments(t *testing.T) {
	t.Parallel()

	store := newTestMemoryStore(t)
	collector := NewCollector(
		store,
		fakeClaimsExtractor{result: claimextract.Result{
			Claims: []models.Claim{{Subject: "bot", Predicate: "stores", Object: "memory", SourceMessageID: "message-1"}},
			Status: "ok",
			Model:  "extract-model",
			Raw:    `{"claims":[{"subject":"bot","predicate":"stores","object":"memory"}]}`,
		}},
		&fakeDocumentIndexer{collectionName: "memory-v1"},
		"embed-model",
		nil,
	)

	message := models.RawMessage{
		MessageID:       "message-1",
		ConversationID:  "channel-1",
		SequenceNumber:  1,
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "The bot stores memory.",
		TimestampUnixMs: 1000,
	}
	if err := collector.IngestMessage(context.Background(), message, CollectOptions{}); err != nil {
		t.Fatalf("IngestMessage() error = %v", err)
	}

	storedMessage, ok, err := store.GetMessageByID(context.Background(), message.MessageID)
	if err != nil || !ok {
		t.Fatalf("GetMessageByID() ok=%v err=%v", ok, err)
	}
	if storedMessage.Content != message.Content {
		t.Fatalf("storedMessage.Content = %q, want %q", storedMessage.Content, message.Content)
	}

	extraction, ok, err := store.GetMessageExtractionByID(context.Background(), message.MessageID)
	if err != nil || !ok {
		t.Fatalf("GetMessageExtractionByID() ok=%v err=%v", ok, err)
	}
	if extraction.Status != "ok" || len(extraction.Claims) != 1 {
		t.Fatalf("unexpected extraction: %#v", extraction)
	}

	claims, err := store.GetClaimsByMessageID(context.Background(), message.MessageID)
	if err != nil {
		t.Fatalf("GetClaimsByMessageID() error = %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("len(claims) = %d, want 1", len(claims))
	}

	documents, err := store.GetVectorDocumentsByMessageID(context.Background(), message.MessageID)
	if err != nil {
		t.Fatalf("GetVectorDocumentsByMessageID() error = %v", err)
	}
	if len(documents) != 2 {
		t.Fatalf("len(documents) = %d, want 2", len(documents))
	}
	for _, doc := range documents {
		if doc.IndexStatus != "indexed" {
			t.Fatalf("document %q status = %q, want indexed", doc.DocumentID, doc.IndexStatus)
		}
	}

	if err := collector.IngestMessage(context.Background(), message, CollectOptions{}); err != nil {
		t.Fatalf("IngestMessage(duplicate) error = %v", err)
	}
	documents, err = store.GetVectorDocumentsByMessageID(context.Background(), message.MessageID)
	if err != nil {
		t.Fatalf("GetVectorDocumentsByMessageID(duplicate) error = %v", err)
	}
	if len(documents) != 2 {
		t.Fatalf("len(documents after duplicate) = %d, want 2", len(documents))
	}
}

func TestCollectorIgnoresBlankAndAssistantMessages(t *testing.T) {
	t.Parallel()

	store := newTestMemoryStore(t)
	indexer := &fakeDocumentIndexer{collectionName: "memory-v1"}
	collector := NewCollector(
		store,
		fakeClaimsExtractor{result: claimextract.Result{Status: "ok", Model: "extract-model"}},
		indexer,
		"embed-model",
		nil,
	)

	if err := collector.IngestMessage(context.Background(), models.RawMessage{
		MessageID:      "message-blank",
		ConversationID: "channel-1",
		AuthorID:       "user-1",
		AuthorRole:     "user",
		Content:        "   ",
	}, CollectOptions{}); err != nil {
		t.Fatalf("IngestMessage(blank) error = %v", err)
	}
	if err := collector.IngestMessage(context.Background(), models.RawMessage{
		MessageID:      "message-assistant",
		ConversationID: "channel-1",
		AuthorID:       "assistant-1",
		AuthorRole:     "assistant",
		Content:        "I am a bot.",
	}, CollectOptions{}); err != nil {
		t.Fatalf("IngestMessage(assistant) error = %v", err)
	}
	if len(indexer.documents) != 0 {
		t.Fatalf("len(indexer.documents) = %d, want 0", len(indexer.documents))
	}
}

func TestCollectorMarksVectorDocumentsFailedWhenIndexingFails(t *testing.T) {
	t.Parallel()

	store := newTestMemoryStore(t)
	collector := NewCollector(
		store,
		fakeClaimsExtractor{result: claimextract.Result{
			Claims: []models.Claim{{Subject: "bot", Predicate: "stores", Object: "memory", SourceMessageID: "message-1"}},
			Status: "ok",
			Model:  "extract-model",
		}},
		&fakeDocumentIndexer{err: fmt.Errorf("qdrant down")},
		"embed-model",
		nil,
	)

	err := collector.IngestMessage(context.Background(), models.RawMessage{
		MessageID:       "message-1",
		ConversationID:  "channel-1",
		SequenceNumber:  1,
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "The bot stores memory.",
		TimestampUnixMs: 1000,
	}, CollectOptions{})
	if err == nil {
		t.Fatal("IngestMessage() error = nil, want indexing failure")
	}

	if _, ok, getErr := store.GetMessageByID(context.Background(), "message-1"); getErr != nil || !ok {
		t.Fatalf("GetMessageByID() ok=%v err=%v", ok, getErr)
	}
	claims, getErr := store.GetClaimsByMessageID(context.Background(), "message-1")
	if getErr != nil {
		t.Fatalf("GetClaimsByMessageID() error = %v", getErr)
	}
	if len(claims) != 1 {
		t.Fatalf("len(claims) = %d, want 1", len(claims))
	}
	documents, getErr := store.GetVectorDocumentsByMessageID(context.Background(), "message-1")
	if getErr != nil {
		t.Fatalf("GetVectorDocumentsByMessageID() error = %v", getErr)
	}
	if len(documents) != 2 {
		t.Fatalf("len(documents) = %d, want 2", len(documents))
	}
	for _, doc := range documents {
		if doc.IndexStatus != "failed" {
			t.Fatalf("document %q status = %q, want failed", doc.DocumentID, doc.IndexStatus)
		}
		if doc.LastError == "" {
			t.Fatalf("document %q last error is empty", doc.DocumentID)
		}
	}
}

func newTestMemoryStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"), nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}

type fakeClaimsExtractor struct {
	result claimextract.Result
}

func (f fakeClaimsExtractor) Extract(context.Context, claimextract.Input) claimextract.Result {
	return f.result
}

type fakeDocumentIndexer struct {
	collectionName string
	documents      []embedding.Document
	err            error
}

func (f *fakeDocumentIndexer) UpsertDocuments(_ context.Context, _ string, docs []embedding.Document) (string, error) {
	f.documents = append(f.documents, docs...)
	if f.err != nil {
		return "", f.err
	}
	return f.collectionName, nil
}
