package telemetry

import (
	"testing"
	"time"
)

func TestApplyEventToSummary(t *testing.T) {
	summary := newTraceSummary("trace-1")
	now := time.Now().UTC()

	applyEventToSummary(&summary, Event{
		TraceID:   "trace-1",
		Timestamp: now,
		Stage:     StageRuntime,
		Kind:      "message_received",
		Payload: map[string]any{
			"message": map[string]any{
				"content":    "hello",
				"author_id":  "user-1",
				"channel_id": "chan-1",
			},
		},
	})
	applyEventToSummary(&summary, Event{
		TraceID:   "trace-1",
		Timestamp: now,
		Stage:     StageConversation,
		Kind:      "parallel_extraction_completed",
		Payload: map[string]any{
			"extractor_statuses": map[string]any{
				"claims":  "ok",
				"topics":  "ok",
				"summary": "ok",
			},
			"message_extraction": map[string]any{
				"message_summary": "The user asked about telemetry debugging.",
			},
		},
	})
	applyEventToSummary(&summary, Event{
		TraceID:   "trace-1",
		Timestamp: now,
		Stage:     StageConversation,
		Kind:      "working_state_updated",
		Payload: map[string]any{
			"summary_update_status": "ok",
			"working_state": map[string]any{
				"rolling_summary": "The conversation is focused on telemetry debugging.",
			},
		},
	})
	applyEventToSummary(&summary, Event{
		TraceID:   "trace-1",
		Timestamp: now,
		Stage:     StageGeneration,
		Kind:      "reply_sent",
		Payload: map[string]any{
			"reply_length": 42,
		},
	})

	if summary.SourceMessage["content"] != "hello" {
		t.Fatalf("source message not captured: %#v", summary.SourceMessage)
	}
	if summary.ExtractorStatuses["topics"] != "ok" {
		t.Fatalf("extractor statuses = %#v, want topics=ok", summary.ExtractorStatuses)
	}
	if summary.WorkingState["rolling_summary"] == "" {
		t.Fatalf("working state not captured: %#v", summary.WorkingState)
	}
	if summary.Status != "completed" {
		t.Fatalf("summary status = %q, want completed", summary.Status)
	}
}

func TestApplyEventToSummaryCapturesErrors(t *testing.T) {
	summary := newTraceSummary("trace-2")
	applyEventToSummary(&summary, Event{
		TraceID:   "trace-2",
		Timestamp: time.Now().UTC(),
		Stage:     StageGeneration,
		Kind:      "generate_error",
		Summary:   "reply generation failed",
		Payload: map[string]any{
			"error": "timeout",
		},
	})

	if summary.Status != "error" {
		t.Fatalf("summary status = %q, want error", summary.Status)
	}
	if len(summary.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(summary.Errors))
	}
}

func TestApplyEventToSummaryCapturesConversationArtifacts(t *testing.T) {
	summary := newTraceSummary("trace-3")
	now := time.Now().UTC()

	applyEventToSummary(&summary, Event{
		TraceID:   "trace-3",
		Timestamp: now,
		Stage:     StageRuntime,
		Kind:      "assistant_message_ingested",
		Payload: map[string]any{
			"message": map[string]any{
				"content":         "Here is the updated summary.",
				"author_role":     "assistant",
				"sequence_number": 8,
			},
		},
	})
	applyEventToSummary(&summary, Event{
		TraceID:   "trace-3",
		Timestamp: now,
		Stage:     StageConversation,
		Kind:      "parallel_extraction_completed",
		Payload: map[string]any{
			"extractor_statuses": map[string]any{
				"claims":  "ok",
				"topics":  "ok",
				"summary": "ok",
			},
			"message_extraction": map[string]any{
				"message_summary": "Assistant updated the summary.",
			},
		},
	})
	applyEventToSummary(&summary, Event{
		TraceID:   "trace-3",
		Timestamp: now,
		Stage:     StageConversation,
		Kind:      "working_state_updated",
		Payload: map[string]any{
			"summary_update_status": "ok",
			"working_state": map[string]any{
				"rolling_summary": "The assistant refined the live conversation state.",
			},
		},
	})
	applyEventToSummary(&summary, Event{
		TraceID:   "trace-3",
		Timestamp: now,
		Stage:     StageConversation,
		Kind:      "response_context_built",
		Payload: map[string]any{
			"response_context": map[string]any{
				"brief": "Current Conversation\nThe assistant refined the live conversation state.",
			},
		},
	})

	if summary.SourceMessage["author_role"] != "assistant" {
		t.Fatalf("assistant source message not captured: %#v", summary.SourceMessage)
	}
	if summary.ExtractorStatuses["topics"] != "ok" {
		t.Fatalf("extractor statuses not captured: %#v", summary.ExtractorStatuses)
	}
	if summary.WorkingState["rolling_summary"] != "The assistant refined the live conversation state." {
		t.Fatalf("working state not captured: %#v", summary.WorkingState)
	}
	if summary.ResponseContext["brief"] == "" {
		t.Fatalf("response context not captured: %#v", summary.ResponseContext)
	}
	if summary.Status != "completed" {
		t.Fatalf("summary status = %q, want completed", summary.Status)
	}
}
