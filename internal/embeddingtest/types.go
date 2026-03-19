package embeddingtest

type MessageSet struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Messages        []MessageEntry `json:"messages"`
	CreatedAtUnixMs int64          `json:"created_at_unix_ms"`
	UpdatedAtUnixMs int64          `json:"updated_at_unix_ms"`
}

type MessageEntry struct {
	Text string `json:"text"`
}

type RunRequest struct {
	MessageSetID   string `json:"message_set_id"`
	EmbeddingModel string `json:"embedding_model"`
	Query          string `json:"query"`
	TopK           int    `json:"top_k"`
}

type RunRecord struct {
	ID                string         `json:"id"`
	MessageSetID      string         `json:"message_set_id"`
	MessageSetName    string         `json:"message_set_name"`
	EmbeddingModel    string         `json:"embedding_model"`
	Query             string         `json:"query"`
	TopK              int            `json:"top_k"`
	Status            string         `json:"status"`
	StartedAtUnixMs   int64          `json:"started_at_unix_ms"`
	CompletedAtUnixMs int64          `json:"completed_at_unix_ms"`
	CollectionName    string         `json:"collection_name"`
	Results           []SearchResult `json:"results"`
	Error             string         `json:"error,omitempty"`
}

type SearchResult struct {
	Rank         int     `json:"rank"`
	Score        float64 `json:"score"`
	MessageIndex int     `json:"message_index"`
	MessageText  string  `json:"message_text"`
}

type Defaults struct {
	EmbeddingModel string `json:"embedding_model"`
}

type ModelOption struct {
	Name string `json:"name"`
}
