package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

const (
	defaultOllamaBaseURL         = "http://localhost:11434"
	defaultChatModel             = "qwen2.5:1.5b-instruct"
	defaultPersona               = "You are a helpful Discord assistant."
	defaultConversationStorePath = "data/conversation-state.json"
	defaultTestSuiteBaseDir      = "data/test-suite"
	defaultWebAddr               = "127.0.0.1:8080"
	defaultRequestTimeoutSec     = 45
	defaultTestSuiteTimeoutSec   = 180
)

type Config struct {
	DiscordBotToken       string
	OllamaBaseURL         string
	OllamaChatModel       string
	OllamaExtractModel    string
	Persona               string
	ConversationStorePath string
	TestSuiteBaseDir      string
	WebAddr               string
	RequestTimeout        time.Duration
	TestSuiteTimeout      time.Duration
	Telemetry             telemetry.Config
}

func Load() (Config, error) {
	token := strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN"))
	if token == "" {
		return Config{}, fmt.Errorf("missing DISCORD_BOT_TOKEN")
	}

	baseURL := readEnvOrDefault("OLLAMA_BASE_URL", defaultOllamaBaseURL)
	chatModel := readEnvOrDefault("OLLAMA_CHAT_MODEL", readEnvOrDefault("OLLAMA_MODEL", defaultChatModel))
	extractModel := readEnvOrDefault("OLLAMA_EXTRACT_MODEL", chatModel)
	persona := readEnvOrDefault("BOT_PERSONA", defaultPersona)
	conversationStorePath := readEnvOrDefault("CONVERSATION_STORE_PATH", defaultConversationStorePath)
	testSuiteBaseDir := readEnvOrDefault("TEST_SUITE_BASE_DIR", defaultTestSuiteBaseDir)
	webAddr := readEnvOrDefault("WEB_ADDR", defaultWebAddr)

	timeoutSec, err := readIntEnv("REQUEST_TIMEOUT_SECONDS", defaultRequestTimeoutSec)
	if err != nil {
		return Config{}, err
	}
	testSuiteTimeoutSec, err := readIntEnv("TEST_SUITE_REQUEST_TIMEOUT_SECONDS", defaultTestSuiteTimeoutSec)
	if err != nil {
		return Config{}, err
	}
	telemetryCfg, err := loadTelemetryConfig()
	if err != nil {
		return Config{}, err
	}

	return Config{
		DiscordBotToken:       token,
		OllamaBaseURL:         strings.TrimRight(baseURL, "/"),
		OllamaChatModel:       chatModel,
		OllamaExtractModel:    extractModel,
		Persona:               persona,
		ConversationStorePath: filepath.Clean(conversationStorePath),
		TestSuiteBaseDir:      filepath.Clean(testSuiteBaseDir),
		WebAddr:               webAddr,
		RequestTimeout:        time.Duration(timeoutSec) * time.Second,
		TestSuiteTimeout:      time.Duration(testSuiteTimeoutSec) * time.Second,
		Telemetry:             telemetryCfg,
	}, nil
}

func loadTelemetryConfig() (telemetry.Config, error) {
	defaults := telemetry.DefaultConfig()

	enabled, err := readBoolEnv("TELEMETRY_ENABLED", defaults.Enabled)
	if err != nil {
		return telemetry.Config{}, err
	}
	enableDiscord, err := readBoolEnv("TELEMETRY_ENABLE_DISCORD_REPORTING", defaults.EnableDiscordReporting)
	if err != nil {
		return telemetry.Config{}, err
	}
	bufferSize, err := readIntEnv("TELEMETRY_BUFFER_SIZE", defaults.BufferSize)
	if err != nil {
		return telemetry.Config{}, err
	}
	maxAttachmentBytes, err := readIntEnv("TELEMETRY_MAX_ATTACHMENT_BYTES", defaults.MaxAttachmentBytes)
	if err != nil {
		return telemetry.Config{}, err
	}
	writePrompts, err := readBoolEnv("TELEMETRY_WRITE_RAW_PROMPT_FILES", defaults.WriteRawPromptFiles)
	if err != nil {
		return telemetry.Config{}, err
	}
	writeResponses, err := readBoolEnv("TELEMETRY_WRITE_RAW_RESPONSE_FILES", defaults.WriteRawResponseFiles)
	if err != nil {
		return telemetry.Config{}, err
	}
	writeStoreEvents, err := readBoolEnv("TELEMETRY_WRITE_STORE_EVENTS", defaults.WriteStoreEvents)
	if err != nil {
		return telemetry.Config{}, err
	}
	writeRuntimeEvents, err := readBoolEnv("TELEMETRY_WRITE_RUNTIME_EVENTS", defaults.WriteRuntimeEvents)
	if err != nil {
		return telemetry.Config{}, err
	}

	return telemetry.Config{
		Enabled:                enabled,
		BaseDir:                filepath.Clean(readEnvOrDefault("TELEMETRY_BASE_DIR", defaults.BaseDir)),
		EnableDiscordReporting: enableDiscord,
		DiscordDebugChannelID:  strings.TrimSpace(readEnvOrDefault("TELEMETRY_DISCORD_DEBUG_CHANNEL_ID", "bot-debug")),
		BufferSize:             bufferSize,
		MaxAttachmentBytes:     maxAttachmentBytes,
		WriteRawPromptFiles:    writePrompts,
		WriteRawResponseFiles:  writeResponses,
		WriteStoreEvents:       writeStoreEvents,
		WriteRuntimeEvents:     writeRuntimeEvents,
	}, nil
}

func readEnvOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func readIntEnv(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", name, raw, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be > 0, got %d", name, value)
	}
	return value, nil
}

func readBoolEnv(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}

	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s=%q: must be a boolean", name, raw)
	}
}
