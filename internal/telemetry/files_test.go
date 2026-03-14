package telemetry

import "testing"

func TestSanitizeFileComponent(t *testing.T) {
	if got := sanitizeFileComponent(" OLLAMA Response / topics "); got != "ollama_response___topics" {
		t.Fatalf("sanitizeFileComponent() = %q", got)
	}

	if got := eventFileName(12, "runtime", "message_received"); got != "012_runtime_message_received.json" {
		t.Fatalf("eventFileName() = %q", got)
	}
}
