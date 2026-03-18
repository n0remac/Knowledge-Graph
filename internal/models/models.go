package models

type RawMessage struct {
	MessageID        string `json:"message_id"`
	ConversationID   string `json:"conversation_id"`
	SequenceNumber   int64  `json:"sequence_number"`
	AuthorID         string `json:"author_id"`
	AuthorRole       string `json:"author_role"`
	Content          string `json:"content"`
	TimestampUnixMs  int64  `json:"timestamp_unix_ms"`
	ReplyToMessageID string `json:"reply_to_message_id"`
}

type Claim struct {
	Subject         string `json:"subject"`
	Predicate       string `json:"predicate"`
	Object          string `json:"object"`
	SourceMessageID string `json:"source_message_id"`
}

type TopicRef struct {
	Name string `json:"name"`
}

type MessageExtraction struct {
	MessageID      string     `json:"message_id"`
	ConversationID string     `json:"conversation_id"`
	Claims         []Claim    `json:"claims"`
	ActiveTopics   []TopicRef `json:"active_topics"`
	MessageSummary string     `json:"message_summary"`

	ClaimsStatus  string `json:"claims_status"`
	TopicsStatus  string `json:"topics_status"`
	SummaryStatus string `json:"summary_status"`

	ClaimsModelVersion  string `json:"claims_model_version"`
	TopicsModelVersion  string `json:"topics_model_version"`
	SummaryModelVersion string `json:"summary_model_version"`

	RawClaimsOutput  string `json:"raw_claims_output"`
	RawTopicsOutput  string `json:"raw_topics_output"`
	RawSummaryOutput string `json:"raw_summary_output"`
}

type TopicState struct {
	Name       string  `json:"name"`
	Salience   float64 `json:"salience"`
	Status     string  `json:"status"`
	LastSeenIn string  `json:"last_seen_in"`
}

type ClaimState struct {
	Subject    string  `json:"subject"`
	Predicate  string  `json:"predicate"`
	Object     string  `json:"object"`
	Salience   float64 `json:"salience"`
	LastSeenIn string  `json:"last_seen_in"`
}

type WorkingState struct {
	ConversationID       string `json:"conversation_id"`
	LastUpdatedMessageID string `json:"last_updated_message_id"`
	StateVersion         int64  `json:"state_version"`

	RollingSummary string       `json:"rolling_summary"`
	ActiveTopics   []TopicState `json:"active_topics"`
	ActiveClaims   []ClaimState `json:"active_claims"`

	RecentMessageIDs    []string `json:"recent_message_ids"`
	RecentExtractionIDs []string `json:"recent_extraction_ids"`

	LastCompactedAtMessage string `json:"last_compacted_at_message"`
}

type ResponseContextArtifact struct {
	MessageID           string   `json:"message_id"`
	ConversationID      string   `json:"conversation_id"`
	WorkingStateVersion int64    `json:"working_state_version"`
	Brief               string   `json:"brief"`
	RecentMessageIDs    []string `json:"recent_message_ids"`
	CreatedAtUnixMs     int64    `json:"created_at_unix_ms"`
}
