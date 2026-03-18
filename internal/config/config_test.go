package config

import "testing"

func clearConfigEnv(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"DISCORD_BOT_TOKEN",
		"OLLAMA_BASE_URL",
		"OLLAMA_CHAT_MODEL",
		"OLLAMA_MODEL",
		"OLLAMA_EXTRACT_MODEL",
		"BOT_PERSONA",
		"CONVERSATION_STORE_PATH",
		"TEST_SUITE_BASE_DIR",
		"WEB_ADDR",
		"REQUEST_TIMEOUT_SECONDS",
		"TEST_SUITE_REQUEST_TIMEOUT_SECONDS",
		"TELEMETRY_ENABLED",
		"TELEMETRY_BASE_DIR",
		"TELEMETRY_ENABLE_DISCORD_REPORTING",
		"TELEMETRY_DISCORD_DEBUG_CHANNEL_ID",
		"TELEMETRY_BUFFER_SIZE",
		"TELEMETRY_MAX_ATTACHMENT_BYTES",
		"TELEMETRY_WRITE_RAW_PROMPT_FILES",
		"TELEMETRY_WRITE_RAW_RESPONSE_FILES",
		"TELEMETRY_WRITE_STORE_EVENTS",
		"TELEMETRY_WRITE_RUNTIME_EVENTS",
	} {
		t.Setenv(name, "")
	}
}

func TestLoadTelemetryDefaults(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Telemetry.Enabled {
		t.Fatalf("expected telemetry enabled by default")
	}
	if cfg.WebAddr != "127.0.0.1:8080" {
		t.Fatalf("WebAddr = %q", cfg.WebAddr)
	}
	if cfg.ConversationStorePath != "data/conversation-state.json" {
		t.Fatalf("ConversationStorePath = %q", cfg.ConversationStorePath)
	}
	if cfg.TestSuiteTimeout.Seconds() != 180 {
		t.Fatalf("TestSuiteTimeout = %s", cfg.TestSuiteTimeout)
	}
	if cfg.Telemetry.BaseDir != "data/telemetry" {
		t.Fatalf("Telemetry.BaseDir = %q", cfg.Telemetry.BaseDir)
	}
	if cfg.Telemetry.BufferSize != 500 {
		t.Fatalf("Telemetry.BufferSize = %d", cfg.Telemetry.BufferSize)
	}
}

func TestLoadTelemetryOverrides(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("TELEMETRY_ENABLED", "false")
	t.Setenv("TELEMETRY_BASE_DIR", "./tmp/traces")
	t.Setenv("TELEMETRY_BUFFER_SIZE", "123")
	t.Setenv("TELEMETRY_MAX_ATTACHMENT_BYTES", "456")
	t.Setenv("TELEMETRY_ENABLE_DISCORD_REPORTING", "false")
	t.Setenv("TELEMETRY_DISCORD_DEBUG_CHANNEL_ID", "debug-1")
	t.Setenv("TELEMETRY_WRITE_RAW_PROMPT_FILES", "false")
	t.Setenv("TELEMETRY_WRITE_RAW_RESPONSE_FILES", "false")
	t.Setenv("TELEMETRY_WRITE_STORE_EVENTS", "false")
	t.Setenv("TELEMETRY_WRITE_RUNTIME_EVENTS", "false")
	t.Setenv("WEB_ADDR", "0.0.0.0:9000")
	t.Setenv("CONVERSATION_STORE_PATH", "./tmp/conversations.json")
	t.Setenv("TEST_SUITE_REQUEST_TIMEOUT_SECONDS", "240")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Telemetry.Enabled {
		t.Fatalf("expected telemetry disabled")
	}
	if cfg.WebAddr != "0.0.0.0:9000" {
		t.Fatalf("WebAddr = %q", cfg.WebAddr)
	}
	if cfg.ConversationStorePath != "tmp/conversations.json" {
		t.Fatalf("ConversationStorePath = %q", cfg.ConversationStorePath)
	}
	if cfg.TestSuiteTimeout.Seconds() != 240 {
		t.Fatalf("TestSuiteTimeout = %s", cfg.TestSuiteTimeout)
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
	if cfg.Telemetry.WriteRawPromptFiles || cfg.Telemetry.WriteRawResponseFiles || cfg.Telemetry.WriteStoreEvents || cfg.Telemetry.WriteRuntimeEvents {
		t.Fatalf("expected write flags to be false: %+v", cfg.Telemetry)
	}
}

func TestLoadDoesNotRequireDiscordToken(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DiscordBotToken != "" {
		t.Fatalf("DiscordBotToken = %q, want empty", cfg.DiscordBotToken)
	}
}

func TestValidateBotConfigRequiresDiscordToken(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := ValidateBotConfig(cfg); err == nil {
		t.Fatal("ValidateBotConfig() error = nil, want missing token error")
	}
}

func TestValidateBotConfigAcceptsLoadedConfigWithToken(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DISCORD_BOT_TOKEN", "token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := ValidateBotConfig(cfg); err != nil {
		t.Fatalf("ValidateBotConfig() error = %v", err)
	}
}

func TestValidateWebConfigAcceptsLoadedConfigWithoutDiscordToken(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := ValidateWebConfig(cfg); err != nil {
		t.Fatalf("ValidateWebConfig() error = %v", err)
	}
}
