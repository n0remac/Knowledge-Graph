package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/conversation"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

type ConversationData struct {
	Conversations []ConversationView `json:"conversations"`
}

type ConversationView struct {
	ConversationID        string                          `json:"conversation_id"`
	MessageCount          int                             `json:"message_count"`
	LatestMessage         models.RawMessage               `json:"latest_message"`
	WorkingState          *models.WorkingState            `json:"working_state,omitempty"`
	RecentMessages        []models.RawMessage             `json:"recent_messages,omitempty"`
	RecentExtractions     []models.MessageExtraction      `json:"recent_extractions,omitempty"`
	LatestResponseContext *models.ResponseContextArtifact `json:"latest_response_context,omitempty"`
	LatestTrace           *TraceArtifactBundle            `json:"latest_trace,omitempty"`
}

type TraceArtifactBundle struct {
	TraceID string                 `json:"trace_id"`
	Index   telemetry.TraceIndex   `json:"index"`
	Summary telemetry.TraceSummary `json:"summary"`
	Events  []TraceArtifact        `json:"events"`
}

type TraceArtifact struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func ConversationDataHandler(cs *conversation.Store, telemetryBaseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if cs == nil {
			http.Error(w, "conversation store unavailable", http.StatusServiceUnavailable)
			return
		}

		snapshot := cs.Snapshot(conversation.SnapshotOptions{
			RecentMessages:    10,
			RecentExtractions: 10,
		})

		data := ConversationData{
			Conversations: make([]ConversationView, 0, len(snapshot.Conversations)),
		}
		for _, convo := range snapshot.Conversations {
			view := ConversationView{
				ConversationID:        convo.ConversationID,
				MessageCount:          convo.MessageCount,
				LatestMessage:         convo.LatestMessage,
				WorkingState:          convo.WorkingState,
				RecentMessages:        convo.RecentMessages,
				RecentExtractions:     convo.RecentExtractions,
				LatestResponseContext: convo.LatestResponseContext,
			}
			if trace, err := loadTraceArtifactBundle(telemetryBaseDir, convo.LatestMessage.MessageID); err == nil {
				view.LatestTrace = trace
			}
			data.Conversations = append(data.Conversations, view)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			http.Error(w, "failed to write conversation data", http.StatusInternalServerError)
			return
		}
	}
}

func loadTraceArtifactBundle(baseDir, traceID string) (*TraceArtifactBundle, error) {
	baseDir = strings.TrimSpace(baseDir)
	traceID = strings.TrimSpace(traceID)
	if baseDir == "" || traceID == "" {
		return nil, errors.New("trace lookup unavailable")
	}

	traceDir := filepath.Join(baseDir, sanitizeFileComponent(traceID))
	indexPath := filepath.Join(traceDir, "trace.json")
	summaryPath := filepath.Join(traceDir, "summary.json")

	var index telemetry.TraceIndex
	if err := decodeJSONFile(indexPath, &index); err != nil {
		return nil, err
	}
	var summary telemetry.TraceSummary
	if err := decodeJSONFile(summaryPath, &summary); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(traceDir)
	if err != nil {
		return nil, err
	}
	artifacts := make([]TraceArtifact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || name == "trace.json" || name == "summary.json" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(traceDir, name))
		if err != nil {
			continue
		}
		artifacts = append(artifacts, TraceArtifact{
			Name:    name,
			Content: string(content),
		})
	}
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Name < artifacts[j].Name
	})

	return &TraceArtifactBundle{
		TraceID: traceID,
		Index:   index,
		Summary: summary,
		Events:  artifacts,
	}, nil
}

func decodeJSONFile(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return errors.New("empty json file")
	}
	return json.Unmarshal(raw, target)
}

func sanitizeFileComponent(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	var builder strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "_.")
}
