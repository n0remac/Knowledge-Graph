package telemetry

import (
	"fmt"
	"strings"
	"time"
)

func newTraceSummary(traceID string) TraceSummary {
	return TraceSummary{
		TraceID: traceID,
		Status:  "in_progress",
	}
}

func applyEventToSummary(summary *TraceSummary, event Event) {
	if summary == nil {
		return
	}

	if summary.TraceID == "" {
		summary.TraceID = event.TraceID
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	summary.UpdatedAt = event.Timestamp

	switch event.Kind {
	case "message_received":
		summary.SourceMessage = cloneMap(payloadMap(event.Payload, "message"))
	case "retrieve_for_extraction":
		summary.ExtractionContext = cloneMap(event.Payload)
	case "retrieve_for_generation":
		summary.GenerationContext = cloneMap(event.Payload)
	case "extract_topics_result":
		summary.TopicCandidates = payloadStrings(event.Payload, "topics")
	case "topics_resolved":
		summary.ResolvedTopics = payloadMaps(event.Payload, "topics")
		if len(summary.ResolvedTopics) == 0 {
			summary.ResolvedTopics = payloadMaps(event.Payload, "resolved_topics")
		}
	case "extract_facts_result":
		summary.FactCandidates = payloadMaps(event.Payload, "facts")
	case "facts_resolved":
		summary.PersistedFacts = payloadMaps(event.Payload, "facts")
		if edges := payloadMaps(event.Payload, "edges"); len(edges) > 0 {
			summary.Edges = edges
		}
	case "generate_reply_result", "reply_generated", "reply_sent":
		summary.Reply = cloneMap(event.Payload)
	}

	if isErrorEvent(event) {
		summary.Errors = append(summary.Errors, map[string]any{
			"stage":     event.Stage,
			"kind":      event.Kind,
			"summary":   event.Summary,
			"error":     payloadString(event.Payload, "error"),
			"timestamp": event.Timestamp,
		})
		summary.Status = "error"
		return
	}

	switch event.Kind {
	case "reply_sent":
		summary.Status = "completed"
	case "reply_generated", "generate_reply_result":
		if summary.Status == "" {
			summary.Status = "in_progress"
		}
	default:
		if summary.Status == "" {
			summary.Status = "in_progress"
		}
	}
}

func isErrorEvent(event Event) bool {
	if strings.Contains(strings.ToLower(strings.TrimSpace(event.Kind)), "error") {
		return true
	}
	if strings.TrimSpace(payloadString(event.Payload, "error")) != "" {
		return true
	}
	return false
}

func payloadMap(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return map[string]any{"value": typed}
	}
}

func payloadMaps(payload map[string]any, key string) []map[string]any {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok {
		return nil
	}

	switch typed := value.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneMap(item))
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			switch mapped := item.(type) {
			case map[string]any:
				out = append(out, cloneMap(mapped))
			default:
				out = append(out, map[string]any{"value": mapped})
			}
		}
		return out
	default:
		return nil
	}
}

func payloadStrings(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok {
		return nil
	}

	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		text := strings.TrimSpace(fmt.Sprint(typed))
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
