package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/researchtest"
)

type ResearchData struct {
	Defaults researchtest.Defaults    `json:"defaults"`
	Runs     []researchtest.RunRecord `json:"runs"`
}

func ResearchDataHandler(service *researchtest.Service, initErr error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := researchServiceError(service, initErr); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		runs, err := service.ListRuns(20)
		if err != nil {
			http.Error(w, "failed to list research runs", http.StatusInternalServerError)
			return
		}

		writeJSONResponse(w, ResearchData{
			Defaults: service.Defaults(),
			Runs:     runs,
		})
	}
}

func ResearchRunHandler(service *researchtest.Service, initErr error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := researchServiceError(service, initErr); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		var request researchtest.RunRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		detail, err := service.RunQuery(r.Context(), request)
		if err != nil {
			writeResearchServiceError(w, err)
			return
		}
		writeJSONResponse(w, detail)
	}
}

func ResearchRunResourceHandler(service *researchtest.Service, initErr error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := researchServiceError(service, initErr); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		resource := strings.TrimPrefix(r.URL.Path, "/research/runs/")
		resource = strings.Trim(resource, "/")
		if resource == "" {
			http.NotFound(w, r)
			return
		}

		detail, err := service.GetRun(resource)
		if err != nil {
			writeResearchServiceError(w, err)
			return
		}
		writeJSONResponse(w, detail)
	}
}

func researchServiceError(service *researchtest.Service, initErr error) error {
	if initErr != nil {
		return initErr
	}
	if service == nil {
		return errors.New("research test service unavailable")
	}
	return nil
}

func writeResearchServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, researchtest.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case isBadRequestError(err):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
