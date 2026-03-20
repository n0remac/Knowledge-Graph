package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

const (
	defaultOllamaBaseURL          = "http://localhost:11434"
	defaultChatModel              = "qwen2.5:1.5b-instruct"
	defaultEmbeddingModel         = "qwen3-embedding:4b"
	defaultPersona                = "You are a helpful Discord assistant."
	defaultConversationStorePath  = "data/conversation-state.json"
	defaultMemoryStorePath        = "data/memory.db"
	defaultTestSuiteBaseDir       = "data/test-suite"
	defaultEmbeddingBaseDir       = "data/embedding-tests"
	defaultWebAddr                = "127.0.0.1:8080"
	defaultRequestTimeoutSec      = 45
	defaultTestSuiteTimeoutSec    = 180
	defaultEmbeddingTimeoutSec    = 180
	defaultQdrantBaseURL          = "http://localhost:6333"
	defaultQdrantCollectionPrefix = "embedding-v1"
)

type Config struct {
	DiscordBotToken        string
	OllamaBaseURL          string
	OllamaChatModel        string
	OllamaExtractModel     string
	OllamaEmbeddingModel   string
	Persona                string
	ConversationStorePath  string
	MemoryObserveChannelID string
	MemoryStorePath        string
	TestSuiteBaseDir       string
	EmbeddingBaseDir       string
	QdrantBaseURL          string
	QdrantAPIKey           string
	QdrantCollectionPrefix string
	WebAddr                string
	RequestTimeout         time.Duration
	TestSuiteTimeout       time.Duration
	EmbeddingTimeout       time.Duration
	Telemetry              telemetry.Config
}

func Load() (Config, error) {
	token := strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN"))

	baseURL := readEnvOrDefault("OLLAMA_BASE_URL", defaultOllamaBaseURL)
	chatModel := readEnvOrDefault("OLLAMA_CHAT_MODEL", readEnvOrDefault("OLLAMA_MODEL", defaultChatModel))
	extractModel := readEnvOrDefault("OLLAMA_EXTRACT_MODEL", chatModel)
	embeddingModel := readEnvOrDefault("OLLAMA_EMBEDDING_MODEL", defaultEmbeddingModel)
	persona := readEnvOrDefault("BOT_PERSONA", defaultPersona)
	conversationStorePath := readEnvOrDefault("CONVERSATION_STORE_PATH", defaultConversationStorePath)
	memoryObserveChannelID := strings.TrimSpace(os.Getenv("MEMORY_OBSERVE_CHANNEL_ID"))
	memoryStorePath := readEnvOrDefault("MEMORY_STORE_PATH", defaultMemoryStorePath)
	testSuiteBaseDir := readEnvOrDefault("TEST_SUITE_BASE_DIR", defaultTestSuiteBaseDir)
	embeddingBaseDir := readEnvOrDefault("EMBEDDING_BASE_DIR", defaultEmbeddingBaseDir)
	qdrantBaseURL := readEnvOrDefault("QDRANT_BASE_URL", defaultQdrantBaseURL)
	qdrantAPIKey := strings.TrimSpace(os.Getenv("QDRANT_API_KEY"))
	qdrantCollectionPrefix := readEnvOrDefault("QDRANT_COLLECTION_PREFIX", defaultQdrantCollectionPrefix)
	webAddr := readEnvOrDefault("WEB_ADDR", defaultWebAddr)

	timeoutSec, err := readIntEnv("REQUEST_TIMEOUT_SECONDS", defaultRequestTimeoutSec)
	if err != nil {
		return Config{}, err
	}
	testSuiteTimeoutSec, err := readIntEnv("TEST_SUITE_REQUEST_TIMEOUT_SECONDS", defaultTestSuiteTimeoutSec)
	if err != nil {
		return Config{}, err
	}
	embeddingTimeoutSec, err := readIntEnv("EMBEDDING_REQUEST_TIMEOUT_SECONDS", defaultEmbeddingTimeoutSec)
	if err != nil {
		return Config{}, err
	}
	telemetryCfg, err := loadTelemetryConfig()
	if err != nil {
		return Config{}, err
	}

	return Config{
		DiscordBotToken:        token,
		OllamaBaseURL:          strings.TrimRight(baseURL, "/"),
		OllamaChatModel:        chatModel,
		OllamaExtractModel:     extractModel,
		OllamaEmbeddingModel:   embeddingModel,
		Persona:                persona,
		ConversationStorePath:  filepath.Clean(conversationStorePath),
		MemoryObserveChannelID: memoryObserveChannelID,
		MemoryStorePath:        filepath.Clean(memoryStorePath),
		TestSuiteBaseDir:       filepath.Clean(testSuiteBaseDir),
		EmbeddingBaseDir:       filepath.Clean(embeddingBaseDir),
		QdrantBaseURL:          strings.TrimRight(strings.TrimSpace(qdrantBaseURL), "/"),
		QdrantAPIKey:           qdrantAPIKey,
		QdrantCollectionPrefix: strings.TrimSpace(qdrantCollectionPrefix),
		WebAddr:                webAddr,
		RequestTimeout:         time.Duration(timeoutSec) * time.Second,
		TestSuiteTimeout:       time.Duration(testSuiteTimeoutSec) * time.Second,
		EmbeddingTimeout:       time.Duration(embeddingTimeoutSec) * time.Second,
		Telemetry:              telemetryCfg,
	}, nil
}

func ValidateBotConfig(cfg Config) error {
	if strings.TrimSpace(cfg.DiscordBotToken) == "" {
		return fmt.Errorf("missing DISCORD_BOT_TOKEN")
	}
	if strings.TrimSpace(cfg.ConversationStorePath) == "" || cfg.ConversationStorePath == "." {
		return fmt.Errorf("conversation store path cannot be empty")
	}
	return validateConversationLLMConfig(cfg.OllamaBaseURL, cfg.OllamaChatModel, cfg.OllamaExtractModel, cfg.Persona, cfg.RequestTimeout)
}

func ValidateWebConfig(cfg Config) error {
	if strings.TrimSpace(cfg.WebAddr) == "" {
		return fmt.Errorf("web addr cannot be empty")
	}
	if strings.TrimSpace(cfg.TestSuiteBaseDir) == "" || cfg.TestSuiteBaseDir == "." {
		return fmt.Errorf("test suite base dir cannot be empty")
	}
	return validateConversationLLMConfig(cfg.OllamaBaseURL, cfg.OllamaChatModel, cfg.OllamaExtractModel, cfg.Persona, cfg.TestSuiteTimeout)
}

func ValidateEmbeddingConfig(cfg Config) error {
	if strings.TrimSpace(cfg.EmbeddingBaseDir) == "" || cfg.EmbeddingBaseDir == "." {
		return fmt.Errorf("embedding base dir cannot be empty")
	}
	if strings.TrimSpace(cfg.OllamaEmbeddingModel) == "" {
		return fmt.Errorf("embedding model cannot be empty")
	}
	if err := validateBaseURL("ollama base url", cfg.OllamaBaseURL); err != nil {
		return err
	}
	if err := validateBaseURL("qdrant base url", cfg.QdrantBaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.QdrantCollectionPrefix) == "" {
		return fmt.Errorf("qdrant collection prefix cannot be empty")
	}
	if cfg.EmbeddingTimeout <= 0 {
		return fmt.Errorf("embedding request timeout must be > 0")
	}
	return nil
}

func ValidateMemoryCollectorConfig(cfg Config) error {
	if strings.TrimSpace(cfg.MemoryStorePath) == "" || cfg.MemoryStorePath == "." {
		return fmt.Errorf("memory store path cannot be empty")
	}
	if strings.TrimSpace(cfg.OllamaEmbeddingModel) == "" {
		return fmt.Errorf("embedding model cannot be empty")
	}
	if err := validateBaseURL("ollama base url", cfg.OllamaBaseURL); err != nil {
		return err
	}
	if err := validateBaseURL("qdrant base url", cfg.QdrantBaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.QdrantCollectionPrefix) == "" {
		return fmt.Errorf("qdrant collection prefix cannot be empty")
	}
	if cfg.EmbeddingTimeout <= 0 {
		return fmt.Errorf("embedding request timeout must be > 0")
	}
	return nil
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

func validateConversationLLMConfig(baseURL, chatModel, extractModel, persona string, timeout time.Duration) error {
	if err := validateBaseURL("ollama base url", baseURL); err != nil {
		return err
	}
	if strings.TrimSpace(chatModel) == "" {
		return fmt.Errorf("chat model cannot be empty")
	}
	if strings.TrimSpace(extractModel) == "" {
		return fmt.Errorf("extract model cannot be empty")
	}
	if strings.TrimSpace(persona) == "" {
		return fmt.Errorf("persona cannot be empty")
	}
	if timeout <= 0 {
		return fmt.Errorf("request timeout must be > 0")
	}
	return nil
}

func validateBaseURL(label, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%s cannot be empty", label)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", label, raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid %s %q: unsupported scheme", label, raw)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid %s %q: host is required", label, raw)
	}
	return nil
}
