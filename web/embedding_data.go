package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/embeddingtest"
)

type EmbeddingData struct {
	Defaults    embeddingtest.Defaults      `json:"defaults"`
	Models      []embeddingtest.ModelOption `json:"models"`
	MessageSets []embeddingtest.MessageSet  `json:"message_sets"`
	Runs        []embeddingtest.RunRecord   `json:"runs"`
}

type embeddingDeleteRequest struct {
	ID string `json:"id"`
}

func EmbeddingDataHandler(service *embeddingtest.Service, initErr error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := embeddingServiceError(service, initErr); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		messageSets, err := service.ListMessageSets()
		if err != nil {
			http.Error(w, "failed to list message sets", http.StatusInternalServerError)
			return
		}
		runs, err := service.ListRuns(20)
		if err != nil {
			http.Error(w, "failed to list runs", http.StatusInternalServerError)
			return
		}
		models, err := service.ListModels(r.Context())
		if err != nil {
			models = nil
		}

		writeJSONResponse(w, EmbeddingData{
			Defaults:    service.Defaults(),
			Models:      models,
			MessageSets: messageSets,
			Runs:        runs,
		})
	}
}

func EmbeddingMessageSetHandler(service *embeddingtest.Service, initErr error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := embeddingServiceError(service, initErr); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		var input embeddingtest.MessageSet
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		set, err := service.SaveMessageSet(input)
		if err != nil {
			writeEmbeddingServiceError(w, err)
			return
		}
		writeJSONResponse(w, set)
	}
}

func EmbeddingMessageSetDeleteHandler(service *embeddingtest.Service, initErr error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := embeddingServiceError(service, initErr); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		var request embeddingDeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := service.DeleteMessageSet(request.ID); err != nil {
			writeEmbeddingServiceError(w, err)
			return
		}
		writeJSONResponse(w, map[string]bool{"deleted": true})
	}
}

func EmbeddingRunHandler(service *embeddingtest.Service, initErr error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := embeddingServiceError(service, initErr); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		var request embeddingtest.RunRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		record, err := service.RunQuery(r.Context(), request)
		if err != nil {
			writeEmbeddingServiceError(w, err)
			return
		}
		writeJSONResponse(w, record)
	}
}

func EmbeddingRunResourceHandler(service *embeddingtest.Service, initErr error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := embeddingServiceError(service, initErr); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		resource := strings.TrimPrefix(r.URL.Path, "/embeddings/runs/")
		resource = strings.Trim(resource, "/")
		if resource == "" {
			http.NotFound(w, r)
			return
		}

		record, err := service.GetRun(resource)
		if err != nil {
			writeEmbeddingServiceError(w, err)
			return
		}
		writeJSONResponse(w, record)
	}
}

func embeddingServiceError(service *embeddingtest.Service, initErr error) error {
	if initErr != nil {
		return initErr
	}
	if service == nil {
		return errors.New("embedding test service unavailable")
	}
	return nil
}

func writeEmbeddingServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, embeddingtest.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case isBadRequestError(err):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
