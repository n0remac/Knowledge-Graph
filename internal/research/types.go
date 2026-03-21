package research

import "context"

const (
	DefaultTopK          = 8
	MaxTopK              = 20
	MaxContextArtifacts  = 6
	MaxEvidenceSnippet   = 280
	memorySourceName     = "memory"
	memorySourceKind     = "internal"
	messageArtifactKind  = "message"
	claimArtifactKind    = "claim"
)

type Source interface {
	Name() string
	Kind() string
}

type SearchableSource interface {
	Source
	Search(context.Context, SearchRequest) ([]ResearchArtifact, error)
}

type FetchableSource interface {
	Source
	Fetch(context.Context, ResearchArtifact) (ResearchArtifact, error)
}

type SearchRequest struct {
	Query          string `json:"query"`
	ConversationID string `json:"conversation_id,omitempty"`
	TopK           int    `json:"top_k"`
}

type PlannedSourceQuery struct {
	SourceName     string `json:"source_name"`
	Query          string `json:"query"`
	ConversationID string `json:"conversation_id,omitempty"`
	TopK           int    `json:"top_k"`
}

type Plan struct {
	Queries []PlannedSourceQuery `json:"queries"`
}

type ResearchProvenance struct {
	RetrievedAtUnixMs int64   `json:"retrieved_at_unix_ms"`
	RetrievedByQuery  string  `json:"retrieved_by_query"`
	SearchRank        int     `json:"search_rank"`
	SearchScore       float64 `json:"search_score"`
	SelectionReason   string  `json:"selection_reason"`
}

type ResearchArtifact struct {
	ArtifactID     string              `json:"artifact_id"`
	SourceName     string              `json:"source_name"`
	SourceKind     string              `json:"source_kind"`
	SourceID       string              `json:"source_id"`
	Kind           string              `json:"kind"`
	Title          string              `json:"title"`
	Content        string              `json:"content"`
	AuthorID       string              `json:"author_id"`
	ConversationID string              `json:"conversation_id"`
	TimestampUnixMs int64              `json:"timestamp_unix_ms"`
	ExternalID     string              `json:"external_id"`
	URL            string              `json:"url"`
	Metadata       map[string]any      `json:"metadata,omitempty"`
	Provenance     ResearchProvenance  `json:"provenance"`
}

type ContextEvidence struct {
	ArtifactID string  `json:"artifact_id"`
	Kind       string  `json:"kind"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
}

type ResearchContext struct {
	Artifacts     []ResearchArtifact `json:"artifacts"`
	Evidence      []ContextEvidence  `json:"evidence"`
	RenderedBrief string             `json:"rendered_brief"`
}
