package conversation

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

func TestEngineResolveReplyTargetUsesStoredMessage(t *testing.T) {
	t.Parallel()

	engine, store := newTestEngine(t)
	ctx := context.Background()

	storedReply, err := store.SaveRawMessage(ctx, models.RawMessage{
		MessageID:       "reply-1",
		ConversationID:  "conversation-1",
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "stored reply",
		TimestampUnixMs: 1000,
	})
	if err != nil {
		t.Fatalf("SaveRawMessage() error = %v", err)
	}

	resolved := engine.resolveReplyTarget(ctx, models.RawMessage{
		MessageID:        "current-1",
		ConversationID:   "conversation-1",
		ReplyToMessageID: storedReply.MessageID,
	}, ProcessOptions{})
	if resolved == nil {
		t.Fatal("resolveReplyTarget() = nil, want stored reply")
	}
	if resolved.MessageID != storedReply.MessageID {
		t.Fatalf("resolveReplyTarget().MessageID = %q, want %q", resolved.MessageID, storedReply.MessageID)
	}
	if resolved.Content != storedReply.Content {
		t.Fatalf("resolveReplyTarget().Content = %q, want %q", resolved.Content, storedReply.Content)
	}
}

func TestEngineResolveReplyTargetPrefersExplicitOption(t *testing.T) {
	t.Parallel()

	engine, store := newTestEngine(t)
	ctx := context.Background()

	if _, err := store.SaveRawMessage(ctx, models.RawMessage{
		MessageID:       "reply-1",
		ConversationID:  "conversation-1",
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "stored reply",
		TimestampUnixMs: 1000,
	}); err != nil {
		t.Fatalf("SaveRawMessage() error = %v", err)
	}

	explicit := &models.RawMessage{
		MessageID:       "explicit-1",
		ConversationID:  "conversation-1",
		AuthorID:        "assistant-1",
		AuthorRole:      "assistant",
		Content:         "explicit reply",
		TimestampUnixMs: 2000,
	}
	resolved := engine.resolveReplyTarget(ctx, models.RawMessage{
		MessageID:        "current-1",
		ConversationID:   "conversation-1",
		ReplyToMessageID: "reply-1",
	}, ProcessOptions{ReplyTarget: explicit})
	if resolved == nil {
		t.Fatal("resolveReplyTarget() = nil, want explicit reply")
	}
	if resolved.MessageID != explicit.MessageID {
		t.Fatalf("resolveReplyTarget().MessageID = %q, want %q", resolved.MessageID, explicit.MessageID)
	}
	if resolved.Content != explicit.Content {
		t.Fatalf("resolveReplyTarget().Content = %q, want %q", resolved.Content, explicit.Content)
	}
}

func TestEngineResolveReplyTargetReturnsNilForMissingOrBlankStoredReply(t *testing.T) {
	t.Parallel()

	engine, store := newTestEngine(t)
	ctx := context.Background()

	if reply := engine.resolveReplyTarget(ctx, models.RawMessage{
		MessageID:        "current-1",
		ConversationID:   "conversation-1",
		ReplyToMessageID: "missing",
	}, ProcessOptions{}); reply != nil {
		t.Fatalf("resolveReplyTarget() = %#v, want nil for missing reply", reply)
	}

	blankReply, err := store.SaveRawMessage(ctx, models.RawMessage{
		MessageID:       "reply-blank",
		ConversationID:  "conversation-1",
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "   ",
		TimestampUnixMs: 1000,
	})
	if err != nil {
		t.Fatalf("SaveRawMessage() error = %v", err)
	}

	if reply := engine.resolveReplyTarget(ctx, models.RawMessage{
		MessageID:        "current-2",
		ConversationID:   "conversation-1",
		ReplyToMessageID: blankReply.MessageID,
	}, ProcessOptions{}); reply != nil {
		t.Fatalf("resolveReplyTarget() = %#v, want nil for blank stored reply", reply)
	}
}

func newTestEngine(t *testing.T) (*Engine, *Store) {
	t.Helper()

	storePath := filepath.Join(t.TempDir(), "conversation.json")
	store, err := NewStore(storePath, nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	return NewEngine(store, nil, "", 0, nil), store
}
