package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

func TestStorePersistsMessagesExtractionsAndVectorDocuments(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "memory.db")
	ctx := context.Background()

	store, err := NewStore(storePath, nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	message, err := store.SaveMessage(ctx, models.RawMessage{
		MessageID:       "message-1",
		ConversationID:  "channel-1",
		SequenceNumber:  1,
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "The bot stores memory.",
		TimestampUnixMs: 1234,
	})
	if err != nil {
		t.Fatalf("SaveMessage() error = %v", err)
	}

	claims, err := store.SaveClaimsExtraction(ctx, ClaimExtractionRecord{
		MessageID:      message.MessageID,
		ConversationID: message.ConversationID,
		Claims: []models.Claim{
			{Subject: "bot", Predicate: "stores", Object: "memory", SourceMessageID: message.MessageID},
		},
		Status:            "ok",
		Model:             "extract-model",
		Raw:               `{"claims":[{"subject":"bot","predicate":"stores","object":"memory"}]}`,
		ExtractedAtUnixMs: 2000,
	})
	if err != nil {
		t.Fatalf("SaveClaimsExtraction() error = %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("len(claims) = %d, want 1", len(claims))
	}

	if err := store.UpsertVectorDocuments(ctx, []VectorDocumentRecord{
		{
			DocumentID:     "message:message-1",
			Kind:           "message",
			MessageID:      message.MessageID,
			ConversationID: message.ConversationID,
			Content:        message.Content,
			EmbeddingModel: "embed-model",
			IndexStatus:    "pending",
			Payload:        map[string]any{"kind": "message"},
		},
		{
			DocumentID:      "claim:" + claims[0].ClaimID,
			Kind:            "claim",
			MessageID:       message.MessageID,
			ClaimID:         claims[0].ClaimID,
			ConversationID:  message.ConversationID,
			Content:         "bot | stores | memory",
			EmbeddingModel:  "embed-model",
			IndexStatus:     "indexed",
			IndexedAtUnixMs: 3000,
			CollectionName:  "memory-v1",
			Payload:         map[string]any{"kind": "claim", "claim_id": claims[0].ClaimID},
		},
	}); err != nil {
		t.Fatalf("UpsertVectorDocuments() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := NewStore(storePath, nil)
	if err != nil {
		t.Fatalf("NewStore(reopen) error = %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Close(reopened) error = %v", err)
		}
	})

	if _, ok, err := reopened.GetMessageByID(ctx, message.MessageID); err != nil || !ok {
		t.Fatalf("GetMessageByID() ok=%v err=%v", ok, err)
	}
	extraction, ok, err := reopened.GetMessageExtractionByID(ctx, message.MessageID)
	if err != nil || !ok {
		t.Fatalf("GetMessageExtractionByID() ok=%v err=%v", ok, err)
	}
	if extraction.Status != "ok" || len(extraction.Claims) != 1 {
		t.Fatalf("unexpected extraction after reopen: %#v", extraction)
	}
	storedClaims, err := reopened.GetClaimsByMessageID(ctx, message.MessageID)
	if err != nil {
		t.Fatalf("GetClaimsByMessageID() error = %v", err)
	}
	if len(storedClaims) != 1 {
		t.Fatalf("len(storedClaims) = %d, want 1", len(storedClaims))
	}
	documents, err := reopened.GetVectorDocumentsByMessageID(ctx, message.MessageID)
	if err != nil {
		t.Fatalf("GetVectorDocumentsByMessageID() error = %v", err)
	}
	if len(documents) != 2 {
		t.Fatalf("len(documents) = %d, want 2", len(documents))
	}
}

func TestStoreSaveMessageWaitsForTransientWriteLock(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "memory.db")
	store, err := NewStore(storePath, nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	locker, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatalf("sql.Open(locker) error = %v", err)
	}
	locker.SetMaxOpenConns(1)
	locker.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := locker.Close(); err != nil {
			t.Fatalf("locker.Close() error = %v", err)
		}
	})

	ctx := context.Background()
	conn, err := locker.Conn(ctx)
	if err != nil {
		t.Fatalf("locker.Conn() error = %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Fatalf("conn.Close() error = %v", err)
		}
	})

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatalf("BEGIN IMMEDIATE error = %v", err)
	}
	t.Cleanup(func() {
		if _, err := conn.ExecContext(context.Background(), `ROLLBACK`); err != nil && err != sql.ErrConnDone {
			// Ignore rollback failures after a successful commit.
		}
	})

	saveErrCh := make(chan error, 1)
	go func() {
		_, saveErr := store.SaveMessage(context.Background(), models.RawMessage{
			MessageID:       "message-locked",
			ConversationID:  "channel-1",
			SequenceNumber:  1,
			AuthorID:        "user-1",
			AuthorRole:      "user",
			Content:         "This write should wait for the lock.",
			TimestampUnixMs: 1234,
		})
		saveErrCh <- saveErr
	}()

	select {
	case err := <-saveErrCh:
		t.Fatalf("SaveMessage() returned before the lock was released: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		t.Fatalf("COMMIT error = %v", err)
	}

	select {
	case err := <-saveErrCh:
		if err != nil {
			t.Fatalf("SaveMessage() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SaveMessage() did not finish after the lock was released")
	}

	if _, ok, err := store.GetMessageByID(ctx, "message-locked"); err != nil || !ok {
		t.Fatalf("GetMessageByID() ok=%v err=%v", ok, err)
	}
}
