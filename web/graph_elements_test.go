package web

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/store"
)

func TestBuildCytoscapeGraph(t *testing.T) {
	t.Parallel()

	graphPath := filepath.Join(t.TempDir(), "graph.json")
	gs, err := store.NewGraph(graphPath, nil)
	if err != nil {
		t.Fatalf("NewGraph() error = %v", err)
	}
	defer func() {
		if err := gs.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	ctx := context.Background()
	now := time.Date(2026, time.March, 14, 18, 0, 0, 0, time.UTC)
	userID := "755274191003058276"

	if err := gs.UpsertUser(ctx, userID, "n0remac6966", "n0remac", now); err != nil {
		t.Fatalf("UpsertUser() error = %v", err)
	}
	if err := gs.SaveMessage(ctx, models.Message{
		ID:        "m1",
		ChannelID: "c1",
		AuthorID:  userID,
		Author:    "n0remac6966",
		Content:   "Knowledge graphs are useful for memory.",
		Timestamp: now,
	}); err != nil {
		t.Fatalf("SaveMessage() error = %v", err)
	}

	topic, err := gs.UpsertTopic(ctx, "knowledge graph", now)
	if err != nil {
		t.Fatalf("UpsertTopic() error = %v", err)
	}
	if err := gs.LinkMessageTopic(ctx, "m1", topic.ID); err != nil {
		t.Fatalf("LinkMessageTopic() error = %v", err)
	}

	fact, err := gs.UpsertFactFromMessage(ctx, models.FactInput{
		DiscordUserID: userID,
		Kind:          "goal",
		ValueText:     "Build a graph viewer",
		AboutType:     "topic",
		AboutID:       int64String(topic.ID),
		Confidence:    0.9,
	}, "m1", "test-model", now)
	if err != nil {
		t.Fatalf("UpsertFactFromMessage() error = %v", err)
	}

	edges := []models.EdgeInput{
		{FromType: "user", FromID: userID, EdgeType: "SENT", ToType: "message", ToID: "m1"},
		{FromType: "message", FromID: "m1", EdgeType: "MENTIONS_TOPIC", ToType: "topic", ToID: int64String(topic.ID)},
		{FromType: "message", FromID: "m1", EdgeType: "DERIVED_FACT", ToType: "fact", ToID: int64String(fact.ID)},
		{FromType: "fact", FromID: int64String(fact.ID), EdgeType: "FACT_FOR_USER", ToType: "user", ToID: userID},
		{FromType: "fact", FromID: int64String(fact.ID), EdgeType: "FACT_ABOUT_TOPIC", ToType: "topic", ToID: int64String(topic.ID)},
		{FromType: "topic", FromID: "404", EdgeType: "FACT_FOR_USER", ToType: "user", ToID: userID},
	}
	for _, input := range edges {
		if _, err := gs.UpsertEdge(ctx, input, now); err != nil {
			t.Fatalf("UpsertEdge(%+v) error = %v", input, err)
		}
	}

	graph, err := BuildCytoscapeGraph(gs)
	if err != nil {
		t.Fatalf("BuildCytoscapeGraph() error = %v", err)
	}

	if got, want := len(graph.Elements.Nodes), 4; got != want {
		t.Fatalf("len(nodes) = %d, want %d", got, want)
	}
	if got, want := len(graph.Elements.Edges), 5; got != want {
		t.Fatalf("len(edges) = %d, want %d", got, want)
	}

	foundMessage := false
	for _, node := range graph.Elements.Nodes {
		if node.Data.ID != "message:m1" {
			continue
		}
		foundMessage = true
		if node.Data.SearchText == "" {
			t.Fatalf("message search text should not be empty")
		}
		if node.Data.Label == "" {
			t.Fatalf("message label should not be empty")
		}
	}
	if !foundMessage {
		t.Fatalf("message node not found")
	}

	for _, edge := range graph.Elements.Edges {
		if edge.Data.Source == "topic:404" {
			t.Fatalf("dangling edge should have been skipped")
		}
	}
}
