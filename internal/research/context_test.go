package research

import (
	"strings"
	"testing"
)

func TestBuildResearchContextUsesFixedSectionOrderAndArtifactLimit(t *testing.T) {
	t.Parallel()

	artifacts := make([]ResearchArtifact, 0, MaxContextArtifacts+1)
	for idx := 0; idx < MaxContextArtifacts+1; idx++ {
		artifacts = append(artifacts, ResearchArtifact{
			ArtifactID: "artifact-" + strings.Repeat("x", idx+1),
			Kind:       messageArtifactKind,
			Content:    "evidence snippet",
			Provenance: ResearchProvenance{SearchScore: float64(MaxContextArtifacts - idx)},
		})
	}

	context := BuildResearchContext(SearchRequest{Query: " find   alpha "}, artifacts)
	if len(context.Artifacts) != MaxContextArtifacts {
		t.Fatalf("len(context.Artifacts) = %d, want %d", len(context.Artifacts), MaxContextArtifacts)
	}
	if !strings.Contains(context.RenderedBrief, "Research Query\nfind alpha\n\nConversation Scope\nglobal\n\nEvidence\n") {
		t.Fatalf("RenderedBrief missing expected sections: %q", context.RenderedBrief)
	}
	if !strings.Contains(context.RenderedBrief, "[message] evidence snippet") {
		t.Fatalf("RenderedBrief missing evidence line: %q", context.RenderedBrief)
	}
}
