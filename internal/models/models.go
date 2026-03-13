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
