package discordbot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/n0remac/Knowledge-Graph/internal/claimextract"
	"github.com/n0remac/Knowledge-Graph/internal/config"
	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
	"github.com/n0remac/Knowledge-Graph/internal/models"
)

func TestOnMessageCreateObservedChannelTriggersMemoryCollector(t *testing.T) {
	t.Parallel()

	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memory.db"), nil)
	if err != nil {
		t.Fatalf("memory.NewStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	collector := memory.NewCollector(
		store,
		runtimeClaimsExtractor{result: claimextract.Result{
			Claims: []models.Claim{{Subject: "bot", Predicate: "stores", Object: "memory", SourceMessageID: "message-1"}},
			Status: "ok",
			Model:  "extract-model",
		}},
		&runtimeDocumentIndexer{collectionName: "memory-v1"},
		"embed-model",
		nil,
	)

	session, err := discordgo.New("Bot token")
	if err != nil {
		t.Fatalf("discordgo.New() error = %v", err)
	}
	session.State = discordgo.NewState()
	if err := session.State.GuildAdd(&discordgo.Guild{
		ID: "guild-1",
		Channels: []*discordgo.Channel{
			{ID: "observe-1", GuildID: "guild-1", Name: "general"},
		},
	}); err != nil {
		t.Fatalf("GuildAdd() error = %v", err)
	}

	runtime := &Runtime{
		cfg:             config.Config{MemoryObserveChannelID: "observe-1", RequestTimeout: 2 * time.Second},
		memoryStore:     store,
		memoryCollector: collector,
	}

	event := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "message-1",
		GuildID:   "guild-1",
		ChannelID: "observe-1",
		Content:   "The bot stores memory.",
		Author:    &discordgo.User{ID: "user-1", Username: "user-1"},
	}}

	runtime.onMessageCreate(session, event)

	message, ok, err := store.GetMessageByID(context.Background(), "message-1")
	if err != nil || !ok {
		t.Fatalf("GetMessageByID() ok=%v err=%v", ok, err)
	}
	if message.Content != "The bot stores memory." {
		t.Fatalf("message.Content = %q", message.Content)
	}
}

func TestOnMessageCreateGeneralChannelTriggersMemoryCollectorByDefault(t *testing.T) {
	t.Parallel()

	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memory.db"), nil)
	if err != nil {
		t.Fatalf("memory.NewStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	collector := memory.NewCollector(
		store,
		runtimeClaimsExtractor{result: claimextract.Result{
			Claims: []models.Claim{{Subject: "general", Predicate: "is", Object: "observed", SourceMessageID: "message-2"}},
			Status: "ok",
			Model:  "extract-model",
		}},
		&runtimeDocumentIndexer{collectionName: "memory-v1"},
		"embed-model",
		nil,
	)

	session, err := discordgo.New("Bot token")
	if err != nil {
		t.Fatalf("discordgo.New() error = %v", err)
	}
	session.State = discordgo.NewState()
	if err := session.State.GuildAdd(&discordgo.Guild{
		ID: "guild-1",
		Channels: []*discordgo.Channel{
			{ID: "general-1", GuildID: "guild-1", Name: "general"},
		},
	}); err != nil {
		t.Fatalf("GuildAdd() error = %v", err)
	}

	runtime := &Runtime{
		cfg:             config.Config{RequestTimeout: 2 * time.Second},
		memoryStore:     store,
		memoryCollector: collector,
	}

	event := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "message-2",
		GuildID:   "guild-1",
		ChannelID: "general-1",
		Content:   "General should be observed.",
		Author:    &discordgo.User{ID: "user-2", Username: "user-2"},
	}}

	runtime.onMessageCreate(session, event)

	message, ok, err := store.GetMessageByID(context.Background(), "message-2")
	if err != nil || !ok {
		t.Fatalf("GetMessageByID() ok=%v err=%v", ok, err)
	}
	if message.Content != "General should be observed." {
		t.Fatalf("message.Content = %q", message.Content)
	}
}

type runtimeClaimsExtractor struct {
	result claimextract.Result
}

func (r runtimeClaimsExtractor) Extract(context.Context, claimextract.Input) claimextract.Result {
	return r.result
}

type runtimeDocumentIndexer struct {
	collectionName string
}

func (r *runtimeDocumentIndexer) UpsertDocuments(context.Context, string, []embedding.Document) (string, error) {
	return r.collectionName, nil
}
