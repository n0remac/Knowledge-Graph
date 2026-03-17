package discordbot

import (
	"github.com/n0remac/Knowledge-Graph/internal/conversation"
	"github.com/n0remac/Knowledge-Graph/internal/store"
)

func (r *Runtime) Store() *store.Store {
	if r == nil {
		return nil
	}
	return r.store
}

func (r *Runtime) ConversationStore() *conversation.Store {
	if r == nil {
		return nil
	}
	return r.conversationStore
}
