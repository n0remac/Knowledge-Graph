package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerDropsWhenBufferFull(t *testing.T) {
	manager := &Manager{
		cfg:                 Config{Enabled: true, WriteRuntimeEvents: true},
		eventsCh:            make(chan Event, 1),
		nextSequenceByTrace: map[string]int64{},
		droppedByTrace:      map[string]int64{},
	}

	ctx := WithTraceID(context.Background(), "trace-1")
	manager.Emit(ctx, StageRuntime, "message_received", "first", nil)
	manager.Emit(ctx, StageRuntime, "reply_sent", "second", nil)

	if got := manager.droppedCount("trace-1"); got != 1 {
		t.Fatalf("dropped count = %d, want 1", got)
	}
}

func TestManagerCloseDrainsToDisk(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewManager(Config{
		Enabled:                true,
		BaseDir:                dir,
		BufferSize:             8,
		WriteRuntimeEvents:     true,
		WriteRawPromptFiles:    true,
		WriteRawResponseFiles:  true,
		WriteStoreEvents:       true,
		WriteRetrievalEvents:   true,
		EnableDiscordReporting: false,
	}, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ctx := WithTraceID(context.Background(), "trace-1")
	manager.Emit(ctx, StageRuntime, "message_received", "received", map[string]any{
		"message": map[string]any{"content": "hello"},
	})
	manager.Emit(ctx, StageGeneration, "reply_sent", "sent", map[string]any{
		"reply_length": 5,
	})

	if err := manager.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	traceDir := filepath.Join(dir, "trace-1")
	if _, err := os.Stat(filepath.Join(traceDir, "001_runtime_message_received.json")); err != nil {
		t.Fatalf("missing first event file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(traceDir, "trace.json")); err != nil {
		t.Fatalf("missing trace.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(traceDir, "summary.json")); err != nil {
		t.Fatalf("missing summary.json: %v", err)
	}
}
