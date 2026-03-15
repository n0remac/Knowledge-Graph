package discordbot

import "github.com/n0remac/Knowledge-Graph/internal/store"

func (r *Runtime) Store() *store.Store {
	if r == nil {
		return nil
	}
	return r.store
}
