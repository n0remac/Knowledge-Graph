package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
