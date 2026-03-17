package models

import "time"

type Message struct {
	ID               string
	ChannelID        string
	GuildID          string
	AuthorID         string
	Author           string
	MentionedUserIDs []string
	Content          string
	Timestamp        time.Time
	ReplyToID        string
}

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

type OpenQuestion struct {
	Text            string `json:"text"`
	SourceMessageID string `json:"source_message_id"`
}

type TopicRef struct {
	Name string `json:"name"`
}

type PronounResolution struct {
	Expression      string  `json:"expression"`
	Referent        string  `json:"referent"`
	Confidence      float64 `json:"confidence"`
	SourceMessageID string  `json:"source_message_id"`
}

type MessageExtraction struct {
	MessageID          string              `json:"message_id"`
	ConversationID     string              `json:"conversation_id"`
	Claims             []Claim             `json:"claims"`
	OpenQuestions      []OpenQuestion      `json:"open_questions"`
	ActiveTopics       []TopicRef          `json:"active_topics"`
	PronounResolutions []PronounResolution `json:"pronoun_resolutions"`
	MessageSummary     string              `json:"message_summary"`

	ClaimsStatus    string `json:"claims_status"`
	QuestionsStatus string `json:"questions_status"`
	TopicsStatus    string `json:"topics_status"`
	PronounsStatus  string `json:"pronouns_status"`
	SummaryStatus   string `json:"summary_status"`

	ClaimsModelVersion    string `json:"claims_model_version"`
	QuestionsModelVersion string `json:"questions_model_version"`
	TopicsModelVersion    string `json:"topics_model_version"`
	PronounsModelVersion  string `json:"pronouns_model_version"`
	SummaryModelVersion   string `json:"summary_model_version"`

	RawClaimsOutput    string `json:"raw_claims_output"`
	RawQuestionsOutput string `json:"raw_questions_output"`
	RawTopicsOutput    string `json:"raw_topics_output"`
	RawPronounsOutput  string `json:"raw_pronouns_output"`
	RawSummaryOutput   string `json:"raw_summary_output"`
}

type TopicState struct {
	Name       string  `json:"name"`
	Salience   float64 `json:"salience"`
	Status     string  `json:"status"`
	LastSeenIn string  `json:"last_seen_in"`
}

type OpenQuestionState struct {
	Text       string  `json:"text"`
	Status     string  `json:"status"`
	Salience   float64 `json:"salience"`
	LastSeenIn string  `json:"last_seen_in"`
}

type ClaimState struct {
	Subject    string  `json:"subject"`
	Predicate  string  `json:"predicate"`
	Object     string  `json:"object"`
	Salience   float64 `json:"salience"`
	LastSeenIn string  `json:"last_seen_in"`
}

type PronounBinding struct {
	Expression string  `json:"expression"`
	Referent   string  `json:"referent"`
	Confidence float64 `json:"confidence"`
	LastSeenIn string  `json:"last_seen_in"`
}

type WorkingState struct {
	ConversationID       string `json:"conversation_id"`
	LastUpdatedMessageID string `json:"last_updated_message_id"`
	StateVersion         int64  `json:"state_version"`

	RollingSummary       string              `json:"rolling_summary"`
	ActiveTopics         []TopicState        `json:"active_topics"`
	OpenQuestions        []OpenQuestionState `json:"open_questions"`
	ActiveClaims         []ClaimState        `json:"active_claims"`
	PronounResolutionMap []PronounBinding    `json:"pronoun_resolution_map"`

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

type Topic struct {
	ID         int64
	Name       string
	Kind       string
	Summary    string
	LastSeenAt time.Time
}

type Fact struct {
	ID            int64
	DiscordUserID string
	Kind          string
	ValueText     string
	AboutType     string
	AboutID       string
	Confidence    float64
	Status        string
	CreatedAt     time.Time
	LastSeenAt    time.Time
}

type FactInput struct {
	DiscordUserID string
	Kind          string
	ValueText     string
	AboutType     string
	AboutID       string
	Confidence    float64
}

type Edge struct {
	ID         int64
	FromType   string
	FromID     string
	EdgeType   string
	ToType     string
	ToID       string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

type EdgeInput struct {
	FromType string
	FromID   string
	EdgeType string
	ToType   string
	ToID     string
}

type ExtractionContext struct {
	RecentMessages     []Message
	RecentTopics       []Topic
	RecentDurableFacts []Fact
	ReplyMessage       *Message
}

type RetrievalBundle struct {
	RecentMessages []Message
	UserFacts      []Fact
	TopicFacts     []Fact
	Topics         []Topic
}
