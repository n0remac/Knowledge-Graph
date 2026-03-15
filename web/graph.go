package web

import (
	"net/http"

	. "github.com/n0remac/GoDom/html"

	"github.com/n0remac/Knowledge-Graph/internal/store"
)

func Graph(mux *http.ServeMux, gs *store.Store) {
	mux.HandleFunc("/graph", ServeNode(GraphPage()))
	mux.HandleFunc("/graph/data", GraphDataHandler(gs))
}
