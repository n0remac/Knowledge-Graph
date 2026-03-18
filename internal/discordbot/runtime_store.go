package discordbot

import (
	"github.com/n0remac/Knowledge-Graph/internal/conversation"
)

func (r *Runtime) ConversationStore() *conversation.Store {
	if r == nil {
		return nil
	}
	return r.conversationStore
}
