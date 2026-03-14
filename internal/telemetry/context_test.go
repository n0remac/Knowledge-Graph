package telemetry

import (
	"context"
	"testing"
)

func TestTraceIDContextHelpers(t *testing.T) {
	ctx := WithTraceID(context.Background(), "12345")
	if got := TraceIDFromContext(ctx); got != "12345" {
		t.Fatalf("TraceIDFromContext() = %q, want %q", got, "12345")
	}

	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Fatalf("TraceIDFromContext() = %q, want empty", got)
	}
}
