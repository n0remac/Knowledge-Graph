package research

import (
	"fmt"
	"sort"
	"strings"
)

type SourceRegistry struct {
	sources map[string]Source
}

func NewSourceRegistry() *SourceRegistry {
	return &SourceRegistry{sources: make(map[string]Source)}
}

func (r *SourceRegistry) Register(source Source) error {
	if r == nil {
		return fmt.Errorf("source registry is not initialized")
	}
	if source == nil {
		return fmt.Errorf("source cannot be nil")
	}
	name := strings.TrimSpace(source.Name())
	if name == "" {
		return fmt.Errorf("source name cannot be empty")
	}
	if _, exists := r.sources[name]; exists {
		return fmt.Errorf("source %q already registered", name)
	}
	r.sources[name] = source
	return nil
}

func (r *SourceRegistry) Source(name string) (Source, bool) {
	if r == nil {
		return nil, false
	}
	source, ok := r.sources[strings.TrimSpace(name)]
	return source, ok
}

func (r *SourceRegistry) Searchable(name string) (SearchableSource, bool) {
	source, ok := r.Source(name)
	if !ok {
		return nil, false
	}
	searchable, ok := source.(SearchableSource)
	return searchable, ok
}

func (r *SourceRegistry) Searchables() []SearchableSource {
	if r == nil {
		return nil
	}
	out := make([]SearchableSource, 0, len(r.sources))
	for _, source := range r.sources {
		searchable, ok := source.(SearchableSource)
		if !ok {
			continue
		}
		out = append(out, searchable)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

func (r *SourceRegistry) Fetchables() []FetchableSource {
	if r == nil {
		return nil
	}
	out := make([]FetchableSource, 0, len(r.sources))
	for _, source := range r.sources {
		fetchable, ok := source.(FetchableSource)
		if !ok {
			continue
		}
		out = append(out, fetchable)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}
