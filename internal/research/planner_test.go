package research

import "testing"

func TestRulePlannerEmitsNormalizedMemoryQuery(t *testing.T) {
	t.Parallel()

	planner := NewRulePlanner(memorySourceName)
	plan, err := planner.Plan(SearchRequest{
		Query:          "  find   alpha   memory ",
		ConversationID: " channel-1 ",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Queries) != 1 {
		t.Fatalf("len(plan.Queries) = %d, want 1", len(plan.Queries))
	}
	query := plan.Queries[0]
	if query.SourceName != memorySourceName {
		t.Fatalf("SourceName = %q", query.SourceName)
	}
	if query.Query != "find alpha memory" {
		t.Fatalf("Query = %q", query.Query)
	}
	if query.ConversationID != "channel-1" {
		t.Fatalf("ConversationID = %q", query.ConversationID)
	}
	if query.TopK != DefaultTopK {
		t.Fatalf("TopK = %d, want %d", query.TopK, DefaultTopK)
	}
}
