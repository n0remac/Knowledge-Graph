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
		Stage:     StageExtraction,
		Kind:      "extract_topics_result",
		Payload: map[string]any{
			"topics": []string{"telemetry", "debugging"},
		},
	})
	applyEventToSummary(&summary, Event{
		TraceID:   "trace-1",
		Timestamp: now,
		Stage:     StageResolution,
		Kind:      "facts_resolved",
		Payload: map[string]any{
			"facts": []map[string]any{
				{"kind": "project", "value_text": "uses traces"},
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
	if len(summary.TopicCandidates) != 2 {
		t.Fatalf("topic candidates = %d, want 2", len(summary.TopicCandidates))
	}
	if len(summary.PersistedFacts) != 1 {
		t.Fatalf("persisted facts = %d, want 1", len(summary.PersistedFacts))
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
