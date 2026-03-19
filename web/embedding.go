package web

import (
	"net/http"

	. "github.com/n0remac/GoDom/html"

	"github.com/n0remac/Knowledge-Graph/internal/embeddingtest"
)

func Embeddings(mux *http.ServeMux, service *embeddingtest.Service, initErr error) {
	mux.HandleFunc("/embeddings", ServeNode(EmbeddingPage()))
	mux.HandleFunc("/embeddings/data", EmbeddingDataHandler(service, initErr))
	mux.HandleFunc("/embeddings/message-sets", EmbeddingMessageSetHandler(service, initErr))
	mux.HandleFunc("/embeddings/message-sets/delete", EmbeddingMessageSetDeleteHandler(service, initErr))
	mux.HandleFunc("/embeddings/run", EmbeddingRunHandler(service, initErr))
	mux.HandleFunc("/embeddings/runs/", EmbeddingRunResourceHandler(service, initErr))
}
