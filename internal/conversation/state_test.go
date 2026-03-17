package conversation

import (
	"testing"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

func TestMergeWorkingStateRefreshesAndResolvesArtifacts(t *testing.T) {
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
		OpenQuestions: []models.OpenQuestionState{
			{Text: "How does the pipeline work?", Status: "open", Salience: 1.00, LastSeenIn: "m1"},
		},
		ActiveClaims: []models.ClaimState{
			{Subject: "pipeline", Predicate: "stores", Object: "messages", Salience: 0.70, LastSeenIn: "m1"},
		},
		PronounResolutionMap: []models.PronounBinding{
			{Expression: "it", Referent: "the pipeline", Confidence: 0.70, LastSeenIn: "m1"},
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
		PronounResolutions: []models.PronounResolution{
			{Expression: "it", Referent: "the pipeline", Confidence: 0.82, SourceMessageID: "m2"},
		},
	}

	sequenceNumbers := map[string]int64{"m1": 1, "m2": 2}
	merged := mergeWorkingState(previous, current, extraction, "The assistant explained the pipeline and rolling summary flow.", func(messageID string) int64 {
		return sequenceNumbers[messageID]
	})

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
	if len(merged.OpenQuestions) == 0 || merged.OpenQuestions[0].Status != "resolved" {
		t.Fatalf("expected question to resolve, got %#v", merged.OpenQuestions)
	}
	if len(merged.ActiveClaims) == 0 || merged.ActiveClaims[0].Salience <= previous.ActiveClaims[0].Salience {
		t.Fatalf("expected active claim salience bump, got %#v", merged.ActiveClaims)
	}
	if len(merged.PronounResolutionMap) == 0 || merged.PronounResolutionMap[0].Confidence < 0.82 {
		t.Fatalf("expected pronoun binding refresh, got %#v", merged.PronounResolutionMap)
	}
	if got := merged.RecentMessageIDs[len(merged.RecentMessageIDs)-1]; got != "m2" {
		t.Fatalf("latest recent message id = %q, want m2", got)
	}
}
