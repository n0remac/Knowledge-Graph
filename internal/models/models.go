package models

import "time"

type Message struct {
	ID        string
	ChannelID string
	GuildID   string
	AuthorID  string
	Author    string
	Content   string
	Timestamp time.Time
	ReplyToID string
}

type Topic struct {
	ID         int64
	Name       string
	Summary    string
	LastSeenAt time.Time
}

type Fact struct {
	ID         int64
	Kind       string
	SubjectID  string
	ObjectID   string
	ValueText  string
	Confidence float64
	Status     string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

type FactInput struct {
	Kind       string
	SubjectID  string
	ObjectID   string
	ValueText  string
	Confidence float64
}

type RetrievalBundle struct {
	RecentMessages []Message
	Facts          []Fact
	Topics         []Topic
}
