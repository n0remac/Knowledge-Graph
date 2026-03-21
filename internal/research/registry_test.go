package research

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
)

func TestSourceRegistryReturnsMemorySourceForSearchCapability(t *testing.T) {
	t.Parallel()

	store := newResearchTestStore(t)
	registry := NewSourceRegistry()
	source := newMemorySourceWithSearcher(store, fakeVectorSearcher{}, "embed-model")
	if err := registry.Register(source); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	searchables := registry.Searchables()
	if len(searchables) != 1 {
		t.Fatalf("len(Searchables()) = %d, want 1", len(searchables))
	}
	if searchables[0].Name() != memorySourceName {
		t.Fatalf("searchables[0].Name() = %q", searchables[0].Name())
	}

	searchable, ok := registry.Searchable(memorySourceName)
	if !ok {
		t.Fatal("Searchable(memory) = not found")
	}
	if searchable.Name() != memorySourceName {
		t.Fatalf("searchable.Name() = %q", searchable.Name())
	}
}

func newResearchTestStore(t *testing.T) *memory.Store {
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

type fakeVectorSearcher struct {
	response embedding.SearchResponse
	filters  map[string]string
	err      error
}

func (f fakeVectorSearcher) Search(context.Context, string, string, int, map[string]string) (embedding.SearchResponse, error) {
	return f.response, f.err
}
