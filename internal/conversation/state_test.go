package conversation

import (
	"strings"
	"testing"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

func TestMergeWorkingStateRefreshesTopicsAndClaims(t *testing.T) {
	t.Parallel()

	previous := models.WorkingState{
		ConversationID:       "c1",
		LastUpdatedMessageID: "m1",
		StateVersion:         1,
		RollingSummary:       "The user asked about the pipeline.",
		ActiveTopics: []models.TopicState{
			{Name: "pipeline", Salience: 0.80, Status: "active", LastSeenIn: "m1"},
			{Name: "older topic", Salience: 0.25, Status: "fading", LastSeenIn: "m1"},
		},
		ActiveClaims: []models.ClaimState{
			{Subject: "pipeline", Predicate: "stores", Object: "messages", Salience: 0.70, LastSeenIn: "m1"},
		},
		RecentMessageIDs:    []string{"m1"},
		RecentExtractionIDs: []string{"m1"},
	}

	current := models.RawMessage{
		MessageID:       "m2",
		ConversationID:  "c1",
		SequenceNumber:  2,
		AuthorID:        "assistant-1",
		AuthorRole:      "assistant",
		Content:         "The pipeline stores messages and updates the rolling summary.",
		TimestampUnixMs: 2000,
	}
	extraction := models.MessageExtraction{
		MessageID:      "m2",
		ConversationID: "c1",
		Claims: []models.Claim{
			{Subject: "pipeline", Predicate: "stores", Object: "messages", SourceMessageID: "m2"},
		},
		ActiveTopics: []models.TopicRef{
			{Name: "pipeline"},
			{Name: "rolling summary"},
		},
	}

	merged := mergeWorkingState(previous, current, extraction, "The assistant explained the pipeline and rolling summary flow.", nil)

	if merged.StateVersion != 2 {
		t.Fatalf("merged.StateVersion = %d, want 2", merged.StateVersion)
	}
	if merged.RollingSummary == previous.RollingSummary {
		t.Fatalf("expected rolling summary to update, got %q", merged.RollingSummary)
	}
	if len(merged.ActiveTopics) < 2 {
		t.Fatalf("expected at least 2 active topics, got %#v", merged.ActiveTopics)
	}
	if merged.ActiveTopics[0].Name != "pipeline" {
		t.Fatalf("top topic = %q, want pipeline", merged.ActiveTopics[0].Name)
	}
	if len(merged.ActiveClaims) == 0 || merged.ActiveClaims[0].Salience <= previous.ActiveClaims[0].Salience {
		t.Fatalf("expected active claim salience bump, got %#v", merged.ActiveClaims)
	}
	if got := merged.RecentMessageIDs[len(merged.RecentMessageIDs)-1]; got != "m2" {
		t.Fatalf("latest recent message id = %q, want m2", got)
	}
	if got := merged.RecentExtractionIDs[len(merged.RecentExtractionIDs)-1]; got != "m2" {
		t.Fatalf("latest recent extraction id = %q, want m2", got)
	}
}

func TestBuildResponseContextArtifactFiltersClaimsByTopic(t *testing.T) {
	t.Parallel()

	message := models.RawMessage{
		MessageID:       "m3",
		ConversationID:  "c1",
		AuthorID:        "user-1",
		AuthorRole:      "user",
		Content:         "How does the rolling summary use the pipeline?",
		TimestampUnixMs: 3000,
	}
	state := models.WorkingState{
		ConversationID:       "c1",
		LastUpdatedMessageID: "m3",
		StateVersion:         3,
		RollingSummary:       "The conversation is focused on the pipeline and rolling summary.",
		ActiveTopics: []models.TopicState{
			{Name: "pipeline", Salience: 0.95, Status: "active", LastSeenIn: "m3"},
			{Name: "rolling summary", Salience: 0.80, Status: "active", LastSeenIn: "m3"},
		},
		ActiveClaims: []models.ClaimState{
			{Subject: "pipeline", Predicate: "stores", Object: "messages", Salience: 0.90, LastSeenIn: "m3"},
			{Subject: "telemetry", Predicate: "writes", Object: "trace files", Salience: 0.88, LastSeenIn: "m3"},
		},
	}
	recentMessages := []models.RawMessage{
		{MessageID: "m2", AuthorID: "assistant-1", AuthorRole: "assistant", Content: "It keeps a short rolling state.", TimestampUnixMs: 2000},
		message,
	}

	artifact := buildResponseContextArtifact(message, state, recentMessages)

	if !strings.Contains(artifact.Brief, "Related Claims") {
		t.Fatalf("brief missing related claims section: %q", artifact.Brief)
	}
	if !strings.Contains(artifact.Brief, "pipeline | stores | messages") {
		t.Fatalf("brief missing topic-related claim: %q", artifact.Brief)
	}
	if strings.Contains(artifact.Brief, "telemetry | writes | trace files") {
		t.Fatalf("brief should not include unrelated claim: %q", artifact.Brief)
	}
}
