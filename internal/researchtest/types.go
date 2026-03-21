package researchtest

import "github.com/n0remac/Knowledge-Graph/internal/research"

type Defaults struct {
	EmbeddingModel string `json:"embedding_model"`
}

type RunRequest struct {
	Query          string `json:"query"`
	ConversationID string `json:"conversation_id"`
	TopK           int    `json:"top_k"`
}

type RunRecord struct {
	ID                   string                   `json:"id"`
	Query                string                   `json:"query"`
	ConversationID       string                   `json:"conversation_id,omitempty"`
	TopK                 int                      `json:"top_k"`
	EmbeddingModel       string                   `json:"embedding_model"`
	Status               string                   `json:"status"`
	StartedAtUnixMs      int64                    `json:"started_at_unix_ms"`
	CompletedAtUnixMs    int64                    `json:"completed_at_unix_ms"`
	Plan                 research.Plan            `json:"plan"`
	ArtifactCount        int                      `json:"artifact_count"`
	SelectedArtifactCount int                     `json:"selected_artifact_count"`
	Context              research.ResearchContext `json:"context"`
	Error                string                   `json:"error,omitempty"`
}

type RunDetail struct {
	Run       RunRecord                   `json:"run"`
	Artifacts []research.ResearchArtifact `json:"artifacts"`
}
