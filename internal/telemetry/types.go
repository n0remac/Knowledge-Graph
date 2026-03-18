package telemetry

import (
	"path/filepath"
	"strings"
	"time"
)

const (
	StageRuntime      = "runtime"
	StageConversation = "conversation"
	StageStore        = "store"
	StageGeneration   = "generation"
	StageDiscordDebug = "discord_debug"

	defaultBaseDir            = "data/telemetry"
	defaultBufferSize         = 500
	defaultMaxAttachmentBytes = 2_000_000
)

type Config struct {
	Enabled                bool
	BaseDir                string
	EnableDiscordReporting bool
	DiscordDebugChannelID  string
	BufferSize             int
	MaxAttachmentBytes     int
	WriteRawPromptFiles    bool
	WriteRawResponseFiles  bool
	WriteStoreEvents       bool
	WriteRuntimeEvents     bool
}

func DefaultConfig() Config {
	return Config{
		Enabled:                true,
		BaseDir:                defaultBaseDir,
		EnableDiscordReporting: true,
		BufferSize:             defaultBufferSize,
		MaxAttachmentBytes:     defaultMaxAttachmentBytes,
		WriteRawPromptFiles:    true,
		WriteRawResponseFiles:  true,
		WriteStoreEvents:       true,
		WriteRuntimeEvents:     true,
	}
}

func (c Config) normalized() Config {
	out := c
	defaults := DefaultConfig()

	if strings.TrimSpace(out.BaseDir) == "" {
		out.BaseDir = defaults.BaseDir
	}
	out.BaseDir = filepath.Clean(out.BaseDir)

	if out.BufferSize <= 0 {
		out.BufferSize = defaults.BufferSize
	}
	if out.MaxAttachmentBytes <= 0 {
		out.MaxAttachmentBytes = defaults.MaxAttachmentBytes
	}
	return out
}

type Event struct {
	TraceID   string         `json:"trace_id"`
	Sequence  int64          `json:"sequence"`
	Timestamp time.Time      `json:"timestamp"`
	Stage     string         `json:"stage"`
	Kind      string         `json:"kind"`
	Summary   string         `json:"summary,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
}

type TraceIndex struct {
	TraceID           string         `json:"trace_id"`
	SourceMessage     map[string]any `json:"source_message,omitempty"`
	EventCount        int64          `json:"event_count"`
	DroppedEventCount int64          `json:"dropped_event_count"`
	LatestStage       string         `json:"latest_stage,omitempty"`
	LatestKind        string         `json:"latest_kind,omitempty"`
	Status            string         `json:"status"`
	StartedAt         time.Time      `json:"started_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type TraceSummary struct {
	TraceID             string            `json:"trace_id"`
	Status              string            `json:"status"`
	SourceMessage       map[string]any    `json:"source_message,omitempty"`
	ExtractorStatuses   map[string]string `json:"extractor_statuses,omitempty"`
	MessageExtraction   map[string]any    `json:"message_extraction,omitempty"`
	WorkingState        map[string]any    `json:"working_state,omitempty"`
	ResponseContext     map[string]any    `json:"response_context,omitempty"`
	SummaryUpdateStatus string            `json:"summary_update_status,omitempty"`
	Reply               map[string]any    `json:"reply,omitempty"`
	Errors              []map[string]any  `json:"errors,omitempty"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

type WrittenEvent struct {
	Event       Event        `json:"event"`
	FilePath    string       `json:"file_path"`
	TraceDir    string       `json:"trace_dir"`
	TracePath   string       `json:"trace_path"`
	SummaryPath string       `json:"summary_path"`
	TraceIndex  TraceIndex   `json:"trace_index"`
	Summary     TraceSummary `json:"summary"`
}

type ReportItem struct {
	TraceID         string   `json:"trace_id"`
	Sequence        int64    `json:"sequence"`
	Kind            string   `json:"kind"`
	Content         string   `json:"content"`
	AttachmentPaths []string `json:"attachment_paths,omitempty"`
}
