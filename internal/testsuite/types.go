package testsuite

type Transcript struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	Steps           []TranscriptStep `json:"steps"`
	CreatedAtUnixMs int64            `json:"created_at_unix_ms"`
	UpdatedAtUnixMs int64            `json:"updated_at_unix_ms"`
}

type TranscriptStep struct {
	Message string `json:"message"`
}

type RunRequest struct {
	TranscriptID string `json:"transcript_id"`
	ChatModel    string `json:"chat_model"`
	ExtractModel string `json:"extract_model"`
	Persona      string `json:"persona"`
}

type RunRecord struct {
	ID                string          `json:"id"`
	TranscriptID      string          `json:"transcript_id"`
	TranscriptName    string          `json:"transcript_name"`
	ChatModel         string          `json:"chat_model"`
	ExtractModel      string          `json:"extract_model"`
	Persona           string          `json:"persona"`
	Status            string          `json:"status"`
	StartedAtUnixMs   int64           `json:"started_at_unix_ms"`
	CompletedAtUnixMs int64           `json:"completed_at_unix_ms"`
	Error             string          `json:"error,omitempty"`
	ConversationID    string          `json:"conversation_id"`
	StorePath         string          `json:"store_path"`
	TelemetryDir      string          `json:"telemetry_dir"`
	Steps             []RunStepResult `json:"steps"`
}

type RunStepResult struct {
	Index               int      `json:"index"`
	UserMessage         string   `json:"user_message"`
	AssistantReply      string   `json:"assistant_reply"`
	SummaryUpdateStatus string   `json:"summary_update_status"`
	WorkingStateVersion int64    `json:"working_state_version"`
	RollingSummary      string   `json:"rolling_summary"`
	ResponseBrief       string   `json:"response_brief"`
	TraceIDs            []string `json:"trace_ids"`
}

type RunDefaults struct {
	ChatModel    string `json:"chat_model"`
	ExtractModel string `json:"extract_model"`
	Persona      string `json:"persona"`
}

type ModelOption struct {
	Name string `json:"name"`
}
