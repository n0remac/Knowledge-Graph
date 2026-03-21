package research

import (
	"fmt"
	"strings"
)

func BuildResearchContext(request SearchRequest, artifacts []ResearchArtifact) ResearchContext {
	query := normalizeQuery(request.Query)
	conversationID := strings.TrimSpace(request.ConversationID)
	if conversationID == "" {
		conversationID = "global"
	}

	selected := selectContextArtifacts(artifacts)
	evidence := make([]ContextEvidence, 0, len(selected))
	lines := make([]string, 0, len(selected))
	for idx, artifact := range selected {
		snippet := buildEvidenceSnippet(artifact.Content)
		evidence = append(evidence, ContextEvidence{
			ArtifactID: artifact.ArtifactID,
			Kind:       artifact.Kind,
			Snippet:    snippet,
			Score:      artifact.Provenance.SearchScore,
		})
		lines = append(lines, fmt.Sprintf("%d. [%s] %s (score=%.3f)", idx+1, artifact.Kind, snippet, artifact.Provenance.SearchScore))
	}
	if len(lines) == 0 {
		lines = append(lines, "- none")
	}

	rendered := fmt.Sprintf(
		"Research Query\n%s\n\nConversation Scope\n%s\n\nEvidence\n%s",
		valueOrDefault(query, "none"),
		conversationID,
		strings.Join(lines, "\n"),
	)

	return ResearchContext{
		Artifacts:     selected,
		Evidence:      evidence,
		RenderedBrief: rendered,
	}
}

func selectContextArtifacts(artifacts []ResearchArtifact) []ResearchArtifact {
	limit := min(len(artifacts), MaxContextArtifacts)
	selected := make([]ResearchArtifact, 0, limit)
	for _, artifact := range artifacts {
		if len(selected) >= MaxContextArtifacts {
			break
		}
		item := artifact
		item.Provenance.SelectionReason = "selected for context preview"
		selected = append(selected, item)
	}
	return selected
}

func buildEvidenceSnippet(input string) string {
	normalized := normalizeQuery(input)
	if normalized == "" {
		return "empty content"
	}
	return truncateRunes(normalized, MaxEvidenceSnippet)
}

func truncateRunes(input string, limit int) string {
	runes := []rune(strings.TrimSpace(input))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func valueOrDefault(input, fallback string) string {
	if strings.TrimSpace(input) == "" {
		return fallback
	}
	return input
}
