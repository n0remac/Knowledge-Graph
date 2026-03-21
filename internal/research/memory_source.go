package research

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/embedding"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
)

type vectorSearcher interface {
	Search(context.Context, string, string, int, map[string]string) (embedding.SearchResponse, error)
}

type MemorySource struct {
	store          *memory.Store
	searcher       vectorSearcher
	embeddingModel string
}

func NewMemorySource(store *memory.Store, index *embedding.Index, embeddingModel string) *MemorySource {
	return newMemorySourceWithSearcher(store, index, embeddingModel)
}

func newMemorySourceWithSearcher(store *memory.Store, searcher vectorSearcher, embeddingModel string) *MemorySource {
	return &MemorySource{
		store:          store,
		searcher:       searcher,
		embeddingModel: strings.TrimSpace(embeddingModel),
	}
}

func (s *MemorySource) Name() string {
	return memorySourceName
}

func (s *MemorySource) Kind() string {
	return memorySourceKind
}

func (s *MemorySource) Search(ctx context.Context, request SearchRequest) ([]ResearchArtifact, error) {
	if s == nil || s.store == nil || s.searcher == nil || s.embeddingModel == "" {
		return nil, fmt.Errorf("memory source is not initialized")
	}

	normalized, err := NormalizeSearchRequest(request)
	if err != nil {
		return nil, err
	}

	var filters map[string]string
	if normalized.ConversationID != "" {
		filters = map[string]string{"conversation_id": normalized.ConversationID}
	}

	response, err := s.searcher.Search(ctx, s.embeddingModel, normalized.Query, normalized.TopK, filters)
	if err != nil {
		return nil, err
	}

	retrievedAt := time.Now().UTC().UnixMilli()
	bestByID := make(map[string]ResearchArtifact, len(response.Results))
	for _, item := range response.Results {
		artifact, err := s.resultToArtifact(ctx, normalized.Query, response.CollectionName, item, retrievedAt)
		if err != nil {
			return nil, err
		}
		existing, ok := bestByID[artifact.ArtifactID]
		if !ok || artifactBetter(artifact, existing) {
			bestByID[artifact.ArtifactID] = artifact
		}
	}

	out := make([]ResearchArtifact, 0, len(bestByID))
	for _, artifact := range bestByID {
		out = append(out, artifact)
	}
	sort.Slice(out, func(i, j int) bool {
		if !almostEqual(out[i].Provenance.SearchScore, out[j].Provenance.SearchScore) {
			return out[i].Provenance.SearchScore > out[j].Provenance.SearchScore
		}
		if out[i].Provenance.SearchRank != out[j].Provenance.SearchRank {
			return out[i].Provenance.SearchRank < out[j].Provenance.SearchRank
		}
		return out[i].ArtifactID < out[j].ArtifactID
	})
	return out, nil
}

func (s *MemorySource) resultToArtifact(ctx context.Context, query, collectionName string, item embedding.SearchResult, retrievedAt int64) (ResearchArtifact, error) {
	kind := strings.TrimSpace(stringValue(item.Payload["kind"]))
	switch kind {
	case messageArtifactKind:
		return s.messageArtifact(query, collectionName, item, retrievedAt)
	case claimArtifactKind:
		return s.claimArtifact(ctx, query, collectionName, item, retrievedAt)
	default:
		return ResearchArtifact{}, fmt.Errorf("unsupported memory artifact kind %q", kind)
	}
}

func (s *MemorySource) messageArtifact(query, collectionName string, item embedding.SearchResult, retrievedAt int64) (ResearchArtifact, error) {
	messageID := strings.TrimSpace(stringValue(item.Payload["message_id"]))
	if messageID == "" {
		return ResearchArtifact{}, fmt.Errorf("message search result missing message_id")
	}

	metadata := cloneMetadata(item.Payload)
	metadata["collection_name"] = collectionName

	content := normalizeQuery(item.Text)
	return ResearchArtifact{
		ArtifactID:      "memory:message:" + messageID,
		SourceName:      memorySourceName,
		SourceKind:      memorySourceKind,
		SourceID:        messageID,
		Kind:            messageArtifactKind,
		Title:           artifactTitle("Message", content),
		Content:         content,
		AuthorID:        stringValue(item.Payload["author_id"]),
		ConversationID:  stringValue(item.Payload["conversation_id"]),
		TimestampUnixMs: int64Value(item.Payload["timestamp_unix_ms"]),
		Metadata:        metadata,
		Provenance: ResearchProvenance{
			RetrievedAtUnixMs: retrievedAt,
			RetrievedByQuery:  query,
			SearchRank:        item.Rank,
			SearchScore:       item.Score,
			SelectionReason:   "retrieved from memory vector search",
		},
	}, nil
}

func (s *MemorySource) claimArtifact(ctx context.Context, query, collectionName string, item embedding.SearchResult, retrievedAt int64) (ResearchArtifact, error) {
	claimID := strings.TrimSpace(stringValue(item.Payload["claim_id"]))
	messageID := strings.TrimSpace(stringValue(item.Payload["message_id"]))
	if claimID == "" || messageID == "" {
		return ResearchArtifact{}, fmt.Errorf("claim search result missing identifiers")
	}

	parentMessage, ok, err := s.store.GetMessageByID(ctx, messageID)
	if err != nil {
		return ResearchArtifact{}, err
	}
	if !ok {
		return ResearchArtifact{}, fmt.Errorf("parent message %q not found for claim %q", messageID, claimID)
	}

	metadata := cloneMetadata(item.Payload)
	metadata["collection_name"] = collectionName
	metadata["parent_message_id"] = parentMessage.MessageID
	metadata["parent_author_role"] = parentMessage.AuthorRole

	content := normalizeQuery(item.Text)
	return ResearchArtifact{
		ArtifactID:      "memory:claim:" + claimID,
		SourceName:      memorySourceName,
		SourceKind:      memorySourceKind,
		SourceID:        claimID,
		Kind:            claimArtifactKind,
		Title:           artifactTitle("Claim", content),
		Content:         content,
		AuthorID:        parentMessage.AuthorID,
		ConversationID:  parentMessage.ConversationID,
		TimestampUnixMs: parentMessage.TimestampUnixMs,
		Metadata:        metadata,
		Provenance: ResearchProvenance{
			RetrievedAtUnixMs: retrievedAt,
			RetrievedByQuery:  query,
			SearchRank:        item.Rank,
			SearchScore:       item.Score,
			SelectionReason:   "retrieved from memory vector search",
		},
	}, nil
}

func artifactTitle(prefix, content string) string {
	content = normalizeQuery(content)
	if content == "" {
		return prefix
	}
	return prefix + ": " + truncateRunes(content, 64)
}

func cloneMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return make(map[string]any)
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func artifactBetter(candidate, current ResearchArtifact) bool {
	if !almostEqual(candidate.Provenance.SearchScore, current.Provenance.SearchScore) {
		return candidate.Provenance.SearchScore > current.Provenance.SearchScore
	}
	if candidate.Provenance.SearchRank != current.Provenance.SearchRank {
		return candidate.Provenance.SearchRank < current.Provenance.SearchRank
	}
	return candidate.ArtifactID < current.ArtifactID
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}
