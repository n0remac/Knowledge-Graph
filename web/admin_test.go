package web

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n0remac/Knowledge-Graph/internal/embeddingtest"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
	"github.com/n0remac/Knowledge-Graph/internal/models"
)

func TestAdminPageIncludesWebsocketAndVerticals(t *testing.T) {
	t.Parallel()

	page := AdminPage(AdminDependencies{})
	rendered := page.Render()

	if !strings.Contains(rendered, `hx-ext="ws"`) {
		t.Fatalf("expected ws extension attribute, got %s", rendered)
	}
	if !strings.Contains(rendered, `ws-connect="/ws/hub?room=admin-dashboard"`) {
		t.Fatalf("expected ws connect attribute, got %s", rendered)
	}
	for _, id := range []string{adminMemorySectionID, adminEmbeddingsSectionID} {
		if !strings.Contains(rendered, `id="`+id+`"`) {
			t.Fatalf("expected section id %q in page", id)
		}
	}
	if strings.Contains(rendered, "admin-vertical-conversation") {
		t.Fatalf("did not expect conversation vertical in page, got %s", rendered)
	}
}

func TestRenderMemorySectionShowsStoredData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	memoryStore, err := memory.NewStore(filepath.Join(t.TempDir(), "memory.db"), nil)
	if err != nil {
		t.Fatalf("memory.NewStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := memoryStore.Close(); err != nil {
			t.Fatalf("memory store Close() error = %v", err)
		}
	})
	memMessage, err := memoryStore.SaveMessage(ctx, models.RawMessage{
		MessageID:       "memory-message-1",
		ConversationID:  "observe-1",
		SequenceNumber:  1,
		AuthorID:        "user-2",
		AuthorRole:      "user",
		Content:         "memory payload",
		TimestampUnixMs: 2000,
	})
	if err != nil {
		t.Fatalf("SaveMessage() error = %v", err)
	}
	claims, err := memoryStore.SaveClaimsExtraction(ctx, memory.ClaimExtractionRecord{
		MessageID:         memMessage.MessageID,
		ConversationID:    memMessage.ConversationID,
		Claims:            []models.Claim{{Subject: "bot", Predicate: "tracks", Object: "memory", SourceMessageID: memMessage.MessageID}},
		Status:            "ok",
		Model:             "extract-model",
		Raw:               `{"claims":[{"subject":"bot","predicate":"tracks","object":"memory"}]}`,
		ExtractedAtUnixMs: 2001,
	})
	if err != nil {
		t.Fatalf("SaveClaimsExtraction() error = %v", err)
	}
	if err := memoryStore.UpsertVectorDocuments(ctx, []memory.VectorDocumentRecord{
		{
			DocumentID:      "message:memory-message-1",
			Kind:            "message",
			MessageID:       memMessage.MessageID,
			ConversationID:  memMessage.ConversationID,
			Content:         memMessage.Content,
			EmbeddingModel:  "embed-model",
			IndexStatus:     "indexed",
			IndexedAtUnixMs: 2002,
			CollectionName:  "memory-v1",
		},
		{
			DocumentID:      "claim:" + claims[0].ClaimID,
			Kind:            "claim",
			MessageID:       memMessage.MessageID,
			ClaimID:         claims[0].ClaimID,
			ConversationID:  memMessage.ConversationID,
			Content:         "bot | tracks | memory",
			EmbeddingModel:  "embed-model",
			IndexStatus:     "indexed",
			IndexedAtUnixMs: 2003,
			CollectionName:  "memory-v1",
		},
	}); err != nil {
		t.Fatalf("UpsertVectorDocuments() error = %v", err)
	}

	memoryRendered := renderMemorySection(memoryStore).Render()
	if !strings.Contains(memoryRendered, "memory payload") {
		t.Fatalf("expected memory message in section, got %s", memoryRendered)
	}
	if !strings.Contains(memoryRendered, "bot | tracks | memory") {
		t.Fatalf("expected claim/vector text in section, got %s", memoryRendered)
	}
}

func TestRenderEmbeddingsSectionShowsServiceData(t *testing.T) {
	t.Parallel()

	service := newWebEmbeddingService(t)
	if _, err := service.SaveMessageSet(embeddingtest.MessageSet{
		Name: "Recall Facts",
		Messages: []embeddingtest.MessageEntry{
			{Text: "alpha"},
			{Text: "beta"},
		},
	}); err != nil {
		t.Fatalf("SaveMessageSet() error = %v", err)
	}

	rendered := renderEmbeddingsSection(nil, service, nil).Render()
	if !strings.Contains(rendered, "Recall Facts") {
		t.Fatalf("expected message set name in embeddings section, got %s", rendered)
	}
	if !strings.Contains(rendered, service.Defaults().EmbeddingModel) {
		t.Fatalf("expected default embedding model in section, got %s", rendered)
	}
}

func TestRenderEmbeddingsSectionShowsLiveVectorData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memory.db"), nil)
	if err != nil {
		t.Fatalf("memory.NewStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("memory store Close() error = %v", err)
		}
	})

	if err := store.UpsertVectorDocuments(ctx, []memory.VectorDocumentRecord{{
		DocumentID:      "message:live-1",
		Kind:            "message",
		MessageID:       "live-1",
		ConversationID:  "observe-1",
		Content:         "indexed memory payload",
		EmbeddingModel:  "qwen3-embedding-4b",
		IndexStatus:     "indexed",
		IndexedAtUnixMs: 3000,
		CollectionName:  "embedding-v1-qwen3-embedding-4b",
	}}); err != nil {
		t.Fatalf("UpsertVectorDocuments() error = %v", err)
	}

	rendered := renderEmbeddingsSection(store, nil, nil).Render()
	if !strings.Contains(rendered, "indexed memory payload") {
		t.Fatalf("expected live vector content in embeddings section, got %s", rendered)
	}
	if !strings.Contains(rendered, "embedding-v1-qwen3-embedding-4b") {
		t.Fatalf("expected collection name in embeddings section, got %s", rendered)
	}
	if !strings.Contains(rendered, "qwen3-embedding-4b") {
		t.Fatalf("expected embedding model in embeddings section, got %s", rendered)
	}
}
