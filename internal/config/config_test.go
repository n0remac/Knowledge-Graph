package config

import "testing"

func TestLoadTelemetryDefaults(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Telemetry.Enabled {
		t.Fatalf("expected telemetry enabled by default")
	}
	if cfg.GraphWebAddr != "127.0.0.1:8080" {
		t.Fatalf("GraphWebAddr = %q", cfg.GraphWebAddr)
	}
	if cfg.Telemetry.BaseDir != "data/telemetry" {
		t.Fatalf("Telemetry.BaseDir = %q", cfg.Telemetry.BaseDir)
	}
	if cfg.Telemetry.BufferSize != 500 {
		t.Fatalf("Telemetry.BufferSize = %d", cfg.Telemetry.BufferSize)
	}
}

func TestLoadTelemetryOverrides(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "token")
	t.Setenv("TELEMETRY_ENABLED", "false")
	t.Setenv("TELEMETRY_BASE_DIR", "./tmp/traces")
	t.Setenv("TELEMETRY_BUFFER_SIZE", "123")
	t.Setenv("TELEMETRY_MAX_ATTACHMENT_BYTES", "456")
	t.Setenv("TELEMETRY_ENABLE_DISCORD_REPORTING", "false")
	t.Setenv("TELEMETRY_DISCORD_DEBUG_CHANNEL_ID", "debug-1")
	t.Setenv("TELEMETRY_WRITE_RAW_PROMPT_FILES", "false")
	t.Setenv("TELEMETRY_WRITE_RAW_RESPONSE_FILES", "false")
	t.Setenv("TELEMETRY_WRITE_STORE_EVENTS", "false")
	t.Setenv("TELEMETRY_WRITE_RETRIEVAL_EVENTS", "false")
	t.Setenv("TELEMETRY_WRITE_RUNTIME_EVENTS", "false")
	t.Setenv("GRAPH_WEB_ADDR", "0.0.0.0:9000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Telemetry.Enabled {
		t.Fatalf("expected telemetry disabled")
	}
	if cfg.GraphWebAddr != "0.0.0.0:9000" {
		t.Fatalf("GraphWebAddr = %q", cfg.GraphWebAddr)
	}
	if cfg.Telemetry.BaseDir != "tmp/traces" {
		t.Fatalf("Telemetry.BaseDir = %q", cfg.Telemetry.BaseDir)
	}
	if cfg.Telemetry.BufferSize != 123 || cfg.Telemetry.MaxAttachmentBytes != 456 {
		t.Fatalf("unexpected telemetry sizes: %+v", cfg.Telemetry)
	}
	if cfg.Telemetry.EnableDiscordReporting {
		t.Fatalf("expected discord reporting disabled")
	}
	if cfg.Telemetry.DiscordDebugChannelID != "debug-1" {
		t.Fatalf("DiscordDebugChannelID = %q", cfg.Telemetry.DiscordDebugChannelID)
	}
	if cfg.Telemetry.WriteRawPromptFiles || cfg.Telemetry.WriteRawResponseFiles || cfg.Telemetry.WriteStoreEvents || cfg.Telemetry.WriteRetrievalEvents || cfg.Telemetry.WriteRuntimeEvents {
		t.Fatalf("expected write flags to be false: %+v", cfg.Telemetry)
	}
}
