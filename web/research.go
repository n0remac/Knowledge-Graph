package web

import (
	"net/http"

	. "github.com/n0remac/GoDom/html"

	"github.com/n0remac/Knowledge-Graph/internal/researchtest"
)

func Research(mux *http.ServeMux, service *researchtest.Service, initErr error) {
	mux.HandleFunc("/research", ServeNode(ResearchPage()))
	mux.HandleFunc("/research/data", ResearchDataHandler(service, initErr))
	mux.HandleFunc("/research/run", ResearchRunHandler(service, initErr))
	mux.HandleFunc("/research/runs/", ResearchRunResourceHandler(service, initErr))
}
