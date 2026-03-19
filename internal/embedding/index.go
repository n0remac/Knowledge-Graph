package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/ollama"
)

var errCollectionNotFound = errors.New("qdrant collection not found")

type Config struct {
	QdrantBaseURL    string
	QdrantAPIKey     string
	CollectionPrefix string
	RequestTimeout   time.Duration
}

type Index struct {
	ollama           *ollama.Client
	qdrant           *qdrantClient
	collectionPrefix string
}

type Document struct {
	MessageSetID string
	MessageIndex int
	MessageText  string
}

type SearchResult struct {
	Rank         int     `json:"rank"`
	Score        float64 `json:"score"`
	MessageIndex int     `json:"message_index"`
	MessageText  string  `json:"message_text"`
}

type SearchResponse struct {
	CollectionName string         `json:"collection_name"`
	Results        []SearchResult `json:"results"`
}

func New(client *ollama.Client, cfg Config) (*Index, error) {
	if client == nil {
		return nil, fmt.Errorf("embedding ollama client is required")
	}
	if strings.TrimSpace(cfg.QdrantBaseURL) == "" {
		return nil, fmt.Errorf("qdrant base url cannot be empty")
	}
	if strings.TrimSpace(cfg.CollectionPrefix) == "" {
		return nil, fmt.Errorf("qdrant collection prefix cannot be empty")
	}
	if cfg.RequestTimeout <= 0 {
		return nil, fmt.Errorf("embedding request timeout must be > 0")
	}

	return &Index{
		ollama: client,
		qdrant: &qdrantClient{
			baseURL: strings.TrimRight(strings.TrimSpace(cfg.QdrantBaseURL), "/"),
			apiKey:  strings.TrimSpace(cfg.QdrantAPIKey),
			http:    &http.Client{Timeout: cfg.RequestTimeout},
		},
		collectionPrefix: sanitizeCollectionPart(cfg.CollectionPrefix),
	}, nil
}

func (i *Index) CollectionName(model string) string {
	return i.collectionPrefix + "-" + sanitizeCollectionPart(model)
}

func (i *Index) IndexMessages(ctx context.Context, model string, docs []Document) (string, error) {
	if i == nil || i.ollama == nil || i.qdrant == nil {
		return "", fmt.Errorf("embedding index is not initialized")
	}

	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("embedding model cannot be empty")
	}
	normalized, err := normalizeDocuments(docs)
	if err != nil {
		return "", err
	}

	vectors := make([][]float64, 0, len(normalized))
	for _, doc := range normalized {
		vector, err := i.ollama.Embed(ctx, model, doc.MessageText)
		if err != nil {
			return "", fmt.Errorf("embed message %d: %w", doc.MessageIndex, err)
		}
		if len(vector) == 0 {
			return "", fmt.Errorf("embed message %d: empty embedding vector", doc.MessageIndex)
		}
		vectors = append(vectors, vector)
	}

	collectionName := i.CollectionName(model)
	if err := i.qdrant.ensureCollection(ctx, collectionName, len(vectors[0])); err != nil {
		return "", err
	}

	points := make([]qdrantPoint, 0, len(normalized))
	for idx, doc := range normalized {
		points = append(points, qdrantPoint{
			ID:     pointID(doc.MessageSetID, doc.MessageIndex),
			Vector: vectors[idx],
			Payload: map[string]any{
				"message_set_id": doc.MessageSetID,
				"message_index":  doc.MessageIndex,
				"message_text":   doc.MessageText,
			},
		})
	}
	if err := i.qdrant.upsertPoints(ctx, collectionName, points); err != nil {
		return "", err
	}
	return collectionName, nil
}

func (i *Index) Search(ctx context.Context, model, messageSetID, query string, topK int) (SearchResponse, error) {
	if i == nil || i.ollama == nil || i.qdrant == nil {
		return SearchResponse{}, fmt.Errorf("embedding index is not initialized")
	}

	model = strings.TrimSpace(model)
	messageSetID = strings.TrimSpace(messageSetID)
	query = strings.TrimSpace(query)
	if model == "" {
		return SearchResponse{}, fmt.Errorf("embedding model cannot be empty")
	}
	if messageSetID == "" {
		return SearchResponse{}, fmt.Errorf("message set id cannot be empty")
	}
	if query == "" {
		return SearchResponse{}, fmt.Errorf("query cannot be empty")
	}
	if topK <= 0 {
		return SearchResponse{}, fmt.Errorf("top_k must be > 0")
	}

	vector, err := i.ollama.Embed(ctx, model, query)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("embed query: %w", err)
	}

	collectionName := i.CollectionName(model)
	hits, err := i.qdrant.searchPoints(ctx, collectionName, messageSetID, vector, topK)
	if err != nil {
		return SearchResponse{}, err
	}

	response := SearchResponse{
		CollectionName: collectionName,
		Results:        make([]SearchResult, 0, len(hits)),
	}
	for idx, hit := range hits {
		response.Results = append(response.Results, SearchResult{
			Rank:         idx + 1,
			Score:        hit.Score,
			MessageIndex: hit.MessageIndex,
			MessageText:  hit.MessageText,
		})
	}
	return response, nil
}

func normalizeDocuments(docs []Document) ([]Document, error) {
	if len(docs) == 0 {
		return nil, fmt.Errorf("messages cannot be empty")
	}

	out := make([]Document, 0, len(docs))
	for _, doc := range docs {
		doc.MessageSetID = strings.TrimSpace(doc.MessageSetID)
		doc.MessageText = strings.TrimSpace(doc.MessageText)
		if doc.MessageSetID == "" {
			return nil, fmt.Errorf("message set id cannot be empty")
		}
		if doc.MessageText == "" {
			return nil, fmt.Errorf("messages cannot contain empty text")
		}
		if doc.MessageIndex < 0 {
			return nil, fmt.Errorf("message index cannot be negative")
		}
		out = append(out, doc)
	}
	return out, nil
}

func sanitizeCollectionPart(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return "default"
	}

	var builder strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if lastDash || builder.Len() == 0 {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	value := strings.Trim(builder.String(), "-")
	if value == "" {
		return "default"
	}
	return value
}

func pointID(messageSetID string, messageIndex int) uint64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(messageSetID))
	_, _ = hasher.Write([]byte{':'})
	_, _ = hasher.Write([]byte(strconv.Itoa(messageIndex)))
	return hasher.Sum64()
}

type qdrantClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type qdrantPoint struct {
	ID      uint64         `json:"id"`
	Vector  []float64      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

type qdrantSearchHit struct {
	Score        float64
	MessageIndex int
	MessageText  string
}

func (c *qdrantClient) ensureCollection(ctx context.Context, collectionName string, vectorSize int) error {
	size, err := c.collectionSize(ctx, collectionName)
	switch {
	case err == nil:
		if size != vectorSize {
			return fmt.Errorf("qdrant collection %q has vector size %d, want %d", collectionName, size, vectorSize)
		}
		return nil
	case errors.Is(err, errCollectionNotFound):
		return c.createCollection(ctx, collectionName, vectorSize)
	default:
		return err
	}
}

func (c *qdrantClient) collectionSize(ctx context.Context, collectionName string) (int, error) {
	resp, err := c.do(ctx, http.MethodGet, "/collections/"+collectionName, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read qdrant collection response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return 0, errCollectionNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("qdrant status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Result struct {
			Config struct {
				Params struct {
					Vectors json.RawMessage `json:"vectors"`
				} `json:"params"`
			} `json:"config"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, fmt.Errorf("decode qdrant collection response: %w", err)
	}
	size, err := parseVectorSize(parsed.Result.Config.Params.Vectors)
	if err != nil {
		return 0, fmt.Errorf("parse qdrant collection size: %w", err)
	}
	return size, nil
}

func parseVectorSize(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("missing vectors configuration")
	}

	var direct struct {
		Size int `json:"size"`
	}
	if err := json.Unmarshal(raw, &direct); err == nil && direct.Size > 0 {
		return direct.Size, nil
	}

	var named map[string]struct {
		Size int `json:"size"`
	}
	if err := json.Unmarshal(raw, &named); err != nil {
		return 0, err
	}
	for _, value := range named {
		if value.Size > 0 {
			return value.Size, nil
		}
	}
	return 0, fmt.Errorf("missing vector size")
}

func (c *qdrantClient) createCollection(ctx context.Context, collectionName string, vectorSize int) error {
	resp, err := c.do(ctx, http.MethodPut, "/collections/"+collectionName, map[string]any{
		"vectors": map[string]any{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read qdrant create collection response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *qdrantClient) upsertPoints(ctx context.Context, collectionName string, points []qdrantPoint) error {
	resp, err := c.do(ctx, http.MethodPut, "/collections/"+collectionName+"/points?wait=true", map[string]any{
		"points": points,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read qdrant upsert response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *qdrantClient) searchPoints(ctx context.Context, collectionName, messageSetID string, vector []float64, topK int) ([]qdrantSearchHit, error) {
	resp, err := c.do(ctx, http.MethodPost, "/collections/"+collectionName+"/points/search", map[string]any{
		"vector":       vector,
		"limit":        topK,
		"with_payload": true,
		"filter": map[string]any{
			"must": []map[string]any{
				{
					"key": "message_set_id",
					"match": map[string]any{
						"value": messageSetID,
					},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read qdrant search response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("qdrant status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Result []struct {
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode qdrant search response: %w", err)
	}

	out := make([]qdrantSearchHit, 0, len(parsed.Result))
	for _, item := range parsed.Result {
		indexValue := 0
		switch value := item.Payload["message_index"].(type) {
		case float64:
			indexValue = int(value)
		case int:
			indexValue = value
		}
		textValue, _ := item.Payload["message_text"].(string)
		out = append(out, qdrantSearchHit{
			Score:        item.Score,
			MessageIndex: indexValue,
			MessageText:  textValue,
		})
	}
	return out, nil
}

func (c *qdrantClient) do(ctx context.Context, method, path string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal qdrant payload: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("create qdrant request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("api-key", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant request failed: %w", err)
	}
	return resp, nil
}
