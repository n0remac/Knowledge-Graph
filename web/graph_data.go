package web

import (
	"encoding/json"
	"net/http"

	"github.com/n0remac/Knowledge-Graph/internal/store"
)

func GraphDataHandler(gs *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		graph, err := BuildCytoscapeGraph(gs)
		if err != nil {
			http.Error(w, "failed to build graph", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(graph); err != nil {
			http.Error(w, "failed to write graph", http.StatusInternalServerError)
			return
		}
	}
}
