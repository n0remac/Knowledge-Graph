package research

import (
	"fmt"
	"strings"
)

type RulePlanner struct {
	sourceName string
}

func NewRulePlanner(sourceName string) *RulePlanner {
	sourceName = strings.TrimSpace(sourceName)
	if sourceName == "" {
		sourceName = memorySourceName
	}
	return &RulePlanner{sourceName: sourceName}
}

func NormalizeSearchRequest(request SearchRequest) (SearchRequest, error) {
	normalized := SearchRequest{
		Query:          normalizeQuery(request.Query),
		ConversationID: strings.TrimSpace(request.ConversationID),
		TopK:           request.TopK,
	}
	if normalized.Query == "" {
		return SearchRequest{}, fmt.Errorf("query cannot be empty")
	}
	if normalized.TopK == 0 {
		normalized.TopK = DefaultTopK
	}
	if normalized.TopK < 1 || normalized.TopK > MaxTopK {
		return SearchRequest{}, fmt.Errorf("top_k must be between 1 and %d", MaxTopK)
	}
	return normalized, nil
}

func (p *RulePlanner) Plan(request SearchRequest) (Plan, error) {
	if p == nil || strings.TrimSpace(p.sourceName) == "" {
		return Plan{}, fmt.Errorf("rule planner is not initialized")
	}

	normalized, err := NormalizeSearchRequest(request)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Queries: []PlannedSourceQuery{{
			SourceName:     p.sourceName,
			Query:          normalized.Query,
			ConversationID: normalized.ConversationID,
			TopK:           normalized.TopK,
		}},
	}, nil
}

func normalizeQuery(input string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
}
