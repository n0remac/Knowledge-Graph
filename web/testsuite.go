package web

import (
	"net/http"

	. "github.com/n0remac/GoDom/html"

	"github.com/n0remac/Knowledge-Graph/internal/testsuite"
)

func TestSuite(mux *http.ServeMux, service *testsuite.Service) {
	mux.HandleFunc("/tests", ServeNode(TestSuitePage()))
	mux.HandleFunc("/tests/data", TestSuiteDataHandler(service))
	mux.HandleFunc("/tests/transcripts", TestSuiteTranscriptHandler(service))
	mux.HandleFunc("/tests/transcripts/delete", TestSuiteTranscriptDeleteHandler(service))
	mux.HandleFunc("/tests/run", TestSuiteRunHandler(service))
	mux.HandleFunc("/tests/runs/", TestSuiteRunResourceHandler(service))
}
