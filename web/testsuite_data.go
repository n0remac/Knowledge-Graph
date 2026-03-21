package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/conversation"
	"github.com/n0remac/Knowledge-Graph/internal/testsuite"
)

type TestSuiteData struct {
	Defaults    testsuite.RunDefaults   `json:"defaults"`
	Models      []testsuite.ModelOption `json:"models"`
	Configs     []testsuite.RunConfig   `json:"configs"`
	Transcripts []testsuite.Transcript  `json:"transcripts"`
	Runs        []testsuite.RunRecord   `json:"runs"`
}

type transcriptDeleteRequest struct {
	ID string `json:"id"`
}

type runConfigDeleteRequest struct {
	ID string `json:"id"`
}

func TestSuiteDataHandler(service *testsuite.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if service == nil {
			http.Error(w, "test suite unavailable", http.StatusServiceUnavailable)
			return
		}

		transcripts, err := service.ListTranscripts()
		if err != nil {
			http.Error(w, "failed to list transcripts", http.StatusInternalServerError)
			return
		}
		runs, err := service.ListRuns(20)
		if err != nil {
			http.Error(w, "failed to list runs", http.StatusInternalServerError)
			return
		}
		models, err := service.ListModels(r.Context())
		if err != nil {
			http.Error(w, "failed to list ollama models", http.StatusInternalServerError)
			return
		}
		configs, err := service.ListRunConfigs()
		if err != nil {
			http.Error(w, "failed to list saved run configs", http.StatusInternalServerError)
			return
		}

		writeJSONResponse(w, TestSuiteData{
			Defaults:    service.Defaults(),
			Models:      models,
			Configs:     configs,
			Transcripts: transcripts,
			Runs:        runs,
		})
	}
}

func TestSuiteRunConfigHandler(service *testsuite.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if service == nil {
			http.Error(w, "test suite unavailable", http.StatusServiceUnavailable)
			return
		}

		var input testsuite.RunConfig
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		config, err := service.SaveRunConfig(input)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSONResponse(w, config)
	}
}

func TestSuiteRunConfigDeleteHandler(service *testsuite.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if service == nil {
			http.Error(w, "test suite unavailable", http.StatusServiceUnavailable)
			return
		}

		var request runConfigDeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := service.DeleteRunConfig(request.ID); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSONResponse(w, map[string]bool{"deleted": true})
	}
}

func TestSuiteTranscriptHandler(service *testsuite.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if service == nil {
			http.Error(w, "test suite unavailable", http.StatusServiceUnavailable)
			return
		}

		var input testsuite.Transcript
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		transcript, err := service.SaveTranscript(input)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSONResponse(w, transcript)
	}
}

func TestSuiteTranscriptDeleteHandler(service *testsuite.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if service == nil {
			http.Error(w, "test suite unavailable", http.StatusServiceUnavailable)
			return
		}

		var request transcriptDeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := service.DeleteTranscript(request.ID); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSONResponse(w, map[string]bool{"deleted": true})
	}
}

func TestSuiteRunHandler(service *testsuite.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if service == nil {
			http.Error(w, "test suite unavailable", http.StatusServiceUnavailable)
			return
		}

		var request testsuite.RunRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		record, err := service.RunTranscript(r.Context(), request)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSONResponse(w, record)
	}
}

func TestSuiteRunResourceHandler(service *testsuite.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			http.Error(w, "test suite unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		resource := strings.TrimPrefix(r.URL.Path, "/tests/runs/")
		resource = strings.Trim(resource, "/")
		if resource == "" {
			http.NotFound(w, r)
			return
		}

		if strings.HasSuffix(resource, "/conversation") {
			runID := strings.TrimSuffix(resource, "/conversation")
			runID = strings.Trim(runID, "/")
			serveTestSuiteRunConversation(w, runID, service)
			return
		}

		record, err := service.GetRun(resource)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSONResponse(w, record)
	}
}

func serveTestSuiteRunConversation(w http.ResponseWriter, runID string, service *testsuite.Service) {
	record, err := service.GetRun(runID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	store, err := conversation.NewStore(record.StorePath, nil)
	if err != nil {
		http.Error(w, "failed to load run conversation store", http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = store.Close()
	}()

	data, err := BuildConversationData(store, record.TelemetryDir)
	if err != nil {
		http.Error(w, "failed to build run conversation data", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, data)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, testsuite.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case isBadRequestError(err):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func isBadRequestError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"required",
		"cannot be empty",
		"must contain",
		"must be between",
		"must be >",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func writeJSONResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "failed to write json response", http.StatusInternalServerError)
	}
}
