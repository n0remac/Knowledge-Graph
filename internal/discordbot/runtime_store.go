package discordbot

import (
	"github.com/n0remac/Knowledge-Graph/internal/conversation"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
)

func (r *Runtime) ConversationStore() *conversation.Store {
	if r == nil {
		return nil
	}
	return r.conversationStore
}

func (r *Runtime) MemoryStore() *memory.Store {
	if r == nil {
		return nil
	}
	return r.memoryStore
}
