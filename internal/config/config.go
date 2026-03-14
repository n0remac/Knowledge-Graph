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
	defaultOllamaBaseURL      = "http://localhost:11434"
	defaultChatModel          = "qwen2.5:1.5b-instruct"
	defaultPersona            = "You are a helpful Discord assistant."
	defaultGraphStorePath     = "data/graph-store.json"
	defaultRecentMessageLimit = 12
	defaultRecallFactLimit    = 12
	defaultRecallTopicLimit   = 8
	defaultRequestTimeoutSec  = 45
)

type Config struct {
	DiscordBotToken    string
	OllamaBaseURL      string
	OllamaChatModel    string
	OllamaExtractModel string
	Persona            string
	GraphStorePath     string
	RecentMessageLimit int
	RecallFactLimit    int
	RecallTopicLimit   int
	RequestTimeout     time.Duration
	Telemetry          telemetry.Config
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
	graphStorePath := readEnvOrDefault("GRAPH_STORE_PATH", readEnvOrDefault("SQLITE_PATH", defaultGraphStorePath))

	recentLimit, err := readIntEnv("RECENT_MESSAGE_LIMIT", defaultRecentMessageLimit)
	if err != nil {
		return Config{}, err
	}
	factLimit, err := readIntEnv("RECALL_FACT_LIMIT", defaultRecallFactLimit)
	if err != nil {
		return Config{}, err
	}
	topicLimit, err := readIntEnv("RECALL_TOPIC_LIMIT", defaultRecallTopicLimit)
	if err != nil {
		return Config{}, err
	}
	timeoutSec, err := readIntEnv("REQUEST_TIMEOUT_SECONDS", defaultRequestTimeoutSec)
	if err != nil {
		return Config{}, err
	}
	telemetryCfg, err := loadTelemetryConfig()
	if err != nil {
		return Config{}, err
	}

	return Config{
		DiscordBotToken:    token,
		OllamaBaseURL:      strings.TrimRight(baseURL, "/"),
		OllamaChatModel:    chatModel,
		OllamaExtractModel: extractModel,
		Persona:            persona,
		GraphStorePath:     filepath.Clean(graphStorePath),
		RecentMessageLimit: recentLimit,
		RecallFactLimit:    factLimit,
		RecallTopicLimit:   topicLimit,
		RequestTimeout:     time.Duration(timeoutSec) * time.Second,
		Telemetry:          telemetryCfg,
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
	writeRetrievalEvents, err := readBoolEnv("TELEMETRY_WRITE_RETRIEVAL_EVENTS", defaults.WriteRetrievalEvents)
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
		WriteRetrievalEvents:   writeRetrievalEvents,
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
