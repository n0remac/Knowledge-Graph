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
	if message := payloadMap(event.Payload, "message"); message != nil {
		summary.SourceMessage = mergeMaps(summary.SourceMessage, message)
	}

	switch event.Kind {
	case "message_received", "assistant_message_ingested":
	case "parallel_extraction_completed", "message_extraction_persisted":
		summary.ExtractorStatuses = payloadStringMap(event.Payload, "extractor_statuses")
		summary.MessageExtraction = cloneMap(payloadMap(event.Payload, "message_extraction"))
	case "working_state_updated":
		summary.WorkingState = cloneMap(payloadMap(event.Payload, "working_state"))
		summary.SummaryUpdateStatus = payloadString(event.Payload, "summary_update_status")
	case "response_context_built":
		summary.ResponseContext = cloneMap(payloadMap(event.Payload, "response_context"))
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
	case "response_context_built":
		if payloadString(summary.SourceMessage, "author_role") == "assistant" {
			summary.Status = "completed"
			return
		}
		if summary.Status == "" {
			summary.Status = "in_progress"
		}
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

func payloadStringMap(payload map[string]any, key string) map[string]string {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case map[string]string:
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = strings.TrimSpace(v)
		}
		return out
	case map[string]any:
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			text := strings.TrimSpace(fmt.Sprint(v))
			if text == "" {
				continue
			}
			out[k] = text
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
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

func mergeMaps(base, update map[string]any) map[string]any {
	if len(base) == 0 {
		return cloneMap(update)
	}
	out := cloneMap(base)
	for key, value := range update {
		out[key] = value
	}
	return out
}
