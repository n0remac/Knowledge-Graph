package web

import (
	"net/http"

	. "github.com/n0remac/GoDom/html"

	"github.com/n0remac/Knowledge-Graph/internal/conversation"
)

func Conversation(mux *http.ServeMux, cs *conversation.Store, telemetryBaseDir string) {
	mux.HandleFunc("/conversation", ServeNode(ConversationPage()))
	mux.HandleFunc("/conversation/data", ConversationDataHandler(cs, telemetryBaseDir))
}
