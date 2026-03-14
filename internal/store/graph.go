package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

const (
	durableConfidenceThreshold = 0.85
	candidateStatus            = "candidate"
	durableStatus              = "durable"
	maxFactValueLength         = 500
)

type Store struct {
	path      string
	mu        sync.RWMutex
	data      graphData
	telemetry *telemetry.Manager
}

type graphData struct {
	Users            map[string]userRecord                 `json:"users"`
	Messages         map[string]models.Message             `json:"messages"`
	ChannelMessageID map[string][]string                   `json:"channel_message_ids"`
	Topics           map[int64]models.Topic                `json:"topics"`
	TopicNameToID    map[string]int64                      `json:"topic_name_to_id"`
	MessageTopics    map[string]map[int64]struct{}         `json:"message_topics"`
	Facts            map[int64]models.Fact                 `json:"facts"`
	FactKeyToID      map[string]int64                      `json:"fact_key_to_id"`
	FactSources      map[int64]map[string]factSourceRecord `json:"fact_sources"`
	Edges            map[int64]models.Edge                 `json:"edges"`
	EdgeKeyToID      map[string]int64                      `json:"edge_key_to_id"`
	NextTopicID      int64                                 `json:"next_topic_id"`
	NextFactID       int64                                 `json:"next_fact_id"`
	NextEdgeID       int64                                 `json:"next_edge_id"`
}

type userRecord struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	UpdatedAt   int64  `json:"updated_at"`
}

type factSourceRecord struct {
	MessageID   string  `json:"message_id"`
	Confidence  float64 `json:"confidence"`
	ExtractedAt int64   `json:"extracted_at"`
	ModelName   string  `json:"model_name"`
}

func NewGraph(path string, manager *telemetry.Manager) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("graph store path cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create graph store directory: %w", err)
	}

	store := &Store{
		path:      path,
		data:      newGraphData(),
		telemetry: manager,
	}

	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

func (s *Store) UpsertUser(ctx context.Context, id, username, displayName string, now time.Time) error {
	input := map[string]any{
		"id":           strings.TrimSpace(id),
		"username":     strings.TrimSpace(username),
		"display_name": strings.TrimSpace(displayName),
	}

	id = strings.TrimSpace(id)
	if id == "" {
		err := fmt.Errorf("user id cannot be empty")
		s.emitError(ctx, "upsert_user_error", "store rejected empty user id", err, map[string]any{"input": input})
		return err
	}
	username = strings.TrimSpace(username)
	if username == "" {
		username = id
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = username
	}

	s.mu.Lock()
	previous, existed := s.data.Users[id]
	user := userRecord{
		ID:          id,
		Username:    username,
		DisplayName: displayName,
		UpdatedAt:   now.UTC().UnixMilli(),
	}
	s.data.Users[id] = user
	s.mu.Unlock()

	s.emit(ctx, "upsert_user", "store upserted user", map[string]any{
		"input":    input,
		"output":   user,
		"created":  !existed,
		"updated":  existed,
		"previous": previous,
	})
	return nil
}

func (s *Store) SaveMessage(ctx context.Context, msg models.Message) error {
	if msg.ID == "" {
		err := fmt.Errorf("message id cannot be empty")
		s.emitError(ctx, "save_message_error", "store rejected empty message id", err, map[string]any{"input": msg})
		return err
	}

	s.mu.Lock()
	_, exists := s.data.Messages[msg.ID]
	if !exists {
		s.data.Messages[msg.ID] = msg
		s.data.ChannelMessageID[msg.ChannelID] = append(s.data.ChannelMessageID[msg.ChannelID], msg.ID)
	}
	count := len(s.data.ChannelMessageID[msg.ChannelID])
	s.mu.Unlock()

	s.emit(ctx, "save_message", "store saved message", map[string]any{
		"input":                   msg,
		"output":                  msg,
		"created":                 !exists,
		"channel_message_count":   count,
		"message_already_present": exists,
	})
	return nil
}

func (s *Store) GetMessageByID(ctx context.Context, messageID string) (models.Message, bool) {
	s.mu.RLock()
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		s.mu.RUnlock()
		s.emit(ctx, "get_message_by_id", "store lookup skipped empty message id", map[string]any{
			"input": map[string]any{"message_id": messageID},
			"found": false,
		})
		return models.Message{}, false
	}
	msg, ok := s.data.Messages[messageID]
	s.mu.RUnlock()

	s.emit(ctx, "get_message_by_id", "store fetched message by id", map[string]any{
		"input":  map[string]any{"message_id": messageID},
		"output": msg,
		"found":  ok,
	})
	return msg, ok
}

func (s *Store) UpsertTopic(ctx context.Context, name string, seenAt time.Time) (models.Topic, error) {
	normalized := normalizeTopic(name)
	if normalized == "" {
		err := fmt.Errorf("topic name is empty")
		s.emitError(ctx, "upsert_topic_error", "store rejected empty topic name", err, map[string]any{"input": map[string]any{"name": name}})
		return models.Topic{}, err
	}

	s.mu.Lock()
	created := false
	var previous models.Topic

	if id, ok := s.data.TopicNameToID[normalized]; ok {
		topic := s.data.Topics[id]
		previous = topic
		if seenAt.After(topic.LastSeenAt) {
			topic.LastSeenAt = seenAt.UTC()
			s.data.Topics[id] = topic
		}
		s.mu.Unlock()
		s.emit(ctx, "upsert_topic", "store updated topic", map[string]any{
			"input":    map[string]any{"name": name, "normalized_name": normalized},
			"output":   topic,
			"created":  false,
			"previous": previous,
		})
		return topic, nil
	}

	s.data.NextTopicID++
	topic := models.Topic{
		ID:         s.data.NextTopicID,
		Name:       normalized,
		Kind:       "other",
		Summary:    "",
		LastSeenAt: seenAt.UTC(),
	}
	s.data.Topics[topic.ID] = topic
	s.data.TopicNameToID[normalized] = topic.ID
	created = true
	s.mu.Unlock()

	s.emit(ctx, "upsert_topic", "store created topic", map[string]any{
		"input":   map[string]any{"name": name, "normalized_name": normalized},
		"output":  topic,
		"created": created,
	})
	return topic, nil
}

func (s *Store) LinkMessageTopic(ctx context.Context, messageID string, topicID int64) error {
	if strings.TrimSpace(messageID) == "" || topicID <= 0 {
		s.emit(ctx, "link_message_topic", "store skipped invalid message-topic link", map[string]any{
			"input": map[string]any{"message_id": messageID, "topic_id": topicID},
			"noop":  true,
		})
		return nil
	}

	s.mu.Lock()
	if _, ok := s.data.Messages[messageID]; !ok {
		s.mu.Unlock()
		err := fmt.Errorf("cannot link unknown message %q", messageID)
		s.emitError(ctx, "link_message_topic_error", "store failed to link message to topic", err, map[string]any{
			"input": map[string]any{"message_id": messageID, "topic_id": topicID},
		})
		return err
	}
	if _, ok := s.data.Topics[topicID]; !ok {
		s.mu.Unlock()
		err := fmt.Errorf("cannot link unknown topic %d", topicID)
		s.emitError(ctx, "link_message_topic_error", "store failed to link message to topic", err, map[string]any{
			"input": map[string]any{"message_id": messageID, "topic_id": topicID},
		})
		return err
	}

	if s.data.MessageTopics[messageID] == nil {
		s.data.MessageTopics[messageID] = make(map[int64]struct{})
	}
	_, existed := s.data.MessageTopics[messageID][topicID]
	s.data.MessageTopics[messageID][topicID] = struct{}{}
	linkCount := len(s.data.MessageTopics[messageID])
	s.mu.Unlock()

	s.emit(ctx, "link_message_topic", "store linked message to topic", map[string]any{
		"input":         map[string]any{"message_id": messageID, "topic_id": topicID},
		"created":       !existed,
		"message_links": linkCount,
	})
	return nil
}

func (s *Store) UpsertFactFromMessage(ctx context.Context, input models.FactInput, messageID, modelName string, observedAt time.Time) (models.Fact, error) {
	clean := sanitizeFactInput(input)
	if clean.Kind == "" || clean.DiscordUserID == "" || clean.AboutType == "" || clean.AboutID == "" || clean.ValueText == "" {
		err := fmt.Errorf("invalid fact input after sanitization")
		s.emitError(ctx, "upsert_fact_error", "store rejected invalid fact input", err, map[string]any{
			"input":           input,
			"sanitized_input": clean,
			"message_id":      messageID,
		})
		return models.Fact{}, err
	}
	if strings.TrimSpace(messageID) == "" {
		err := fmt.Errorf("message id cannot be empty for fact source")
		s.emitError(ctx, "upsert_fact_error", "store rejected fact source without message id", err, map[string]any{
			"input":           input,
			"sanitized_input": clean,
			"message_id":      messageID,
		})
		return models.Fact{}, err
	}

	s.mu.Lock()
	factKey := buildFactKey(clean)
	factID, exists := s.data.FactKeyToID[factKey]
	now := observedAt.UTC()
	created := false
	var previous models.Fact
	if !exists {
		s.data.NextFactID++
		factID = s.data.NextFactID
		status := candidateStatus
		if clean.Confidence >= durableConfidenceThreshold {
			status = durableStatus
		}
		s.data.Facts[factID] = models.Fact{
			ID:            factID,
			DiscordUserID: clean.DiscordUserID,
			Kind:          clean.Kind,
			ValueText:     clean.ValueText,
			AboutType:     clean.AboutType,
			AboutID:       clean.AboutID,
			Confidence:    clean.Confidence,
			Status:        status,
			CreatedAt:     now,
			LastSeenAt:    now,
		}
		s.data.FactKeyToID[factKey] = factID
		created = true
	} else {
		fact := s.data.Facts[factID]
		previous = fact
		if clean.Confidence > fact.Confidence {
			fact.Confidence = clean.Confidence
		}
		fact.ValueText = clean.ValueText
		fact.LastSeenAt = now
		if fact.Confidence >= durableConfidenceThreshold {
			fact.Status = durableStatus
		}
		s.data.Facts[factID] = fact
	}

	if s.data.FactSources[factID] == nil {
		s.data.FactSources[factID] = make(map[string]factSourceRecord)
	}
	s.data.FactSources[factID][messageID] = factSourceRecord{
		MessageID:   messageID,
		Confidence:  clean.Confidence,
		ExtractedAt: now.UnixMilli(),
		ModelName:   strings.TrimSpace(modelName),
	}

	fact := s.data.Facts[factID]
	if len(s.data.FactSources[factID]) >= 2 && fact.Status != durableStatus {
		fact.Status = durableStatus
		s.data.Facts[factID] = fact
	}
	output := s.data.Facts[factID]
	sourceCount := len(s.data.FactSources[factID])
	s.mu.Unlock()

	s.emit(ctx, "upsert_fact", "store upserted fact", map[string]any{
		"input":           input,
		"sanitized_input": clean,
		"output":          output,
		"created":         created,
		"previous":        previous,
		"message_id":      messageID,
		"model_name":      strings.TrimSpace(modelName),
		"source_count":    sourceCount,
	})

	return output, nil
}

func (s *Store) UpsertEdge(ctx context.Context, input models.EdgeInput, observedAt time.Time) (models.Edge, error) {
	clean := sanitizeEdgeInput(input)
	if clean.FromType == "" || clean.FromID == "" || clean.EdgeType == "" || clean.ToType == "" || clean.ToID == "" {
		err := fmt.Errorf("invalid edge input after sanitization")
		s.emitError(ctx, "upsert_edge_error", "store rejected invalid edge input", err, map[string]any{
			"input":           input,
			"sanitized_input": clean,
		})
		return models.Edge{}, err
	}

	s.mu.Lock()
	now := observedAt.UTC()
	edgeKey := buildEdgeKey(clean)
	edgeID, exists := s.data.EdgeKeyToID[edgeKey]
	created := false
	if !exists {
		s.data.NextEdgeID++
		edgeID = s.data.NextEdgeID
		s.data.Edges[edgeID] = models.Edge{
			ID:         edgeID,
			FromType:   clean.FromType,
			FromID:     clean.FromID,
			EdgeType:   clean.EdgeType,
			ToType:     clean.ToType,
			ToID:       clean.ToID,
			CreatedAt:  now,
			LastSeenAt: now,
		}
		s.data.EdgeKeyToID[edgeKey] = edgeID
		created = true
		edge := s.data.Edges[edgeID]
		s.mu.Unlock()
		s.emit(ctx, "upsert_edge", "store created edge", map[string]any{
			"input":           input,
			"sanitized_input": clean,
			"output":          edge,
			"created":         created,
		})
		return edge, nil
	}

	edge := s.data.Edges[edgeID]
	edge.LastSeenAt = now
	s.data.Edges[edgeID] = edge
	s.mu.Unlock()

	s.emit(ctx, "upsert_edge", "store updated edge", map[string]any{
		"input":           input,
		"sanitized_input": clean,
		"output":          edge,
		"created":         false,
	})
	return edge, nil
}

func (s *Store) GetRecentMessages(ctx context.Context, channelID string, limit int) ([]models.Message, error) {
	s.mu.RLock()
	messageIDs := s.data.ChannelMessageID[channelID]
	if len(messageIDs) == 0 {
		s.mu.RUnlock()
		s.emit(ctx, "get_recent_messages", "store fetched recent messages", map[string]any{
			"input":  map[string]any{"channel_id": channelID, "limit": limit},
			"count":  0,
			"output": []models.Message{},
		})
		return nil, nil
	}

	start := 0
	if len(messageIDs) > limit {
		start = len(messageIDs) - limit
	}

	out := make([]models.Message, 0, len(messageIDs)-start)
	for _, id := range messageIDs[start:] {
		out = append(out, s.data.Messages[id])
	}
	s.mu.RUnlock()

	s.emit(ctx, "get_recent_messages", "store fetched recent messages", map[string]any{
		"input":  map[string]any{"channel_id": channelID, "limit": limit},
		"count":  len(out),
		"output": out,
	})
	return out, nil
}

func (s *Store) GetDurableFactsForDiscordUser(ctx context.Context, discordUserID string, limit int) ([]models.Fact, error) {
	discordUserID = normalizeDiscordUserID(discordUserID)
	if discordUserID == "" {
		s.emit(ctx, "get_durable_facts_for_discord_user", "store skipped durable fact lookup for empty user id", map[string]any{
			"input":  map[string]any{"discord_user_id": discordUserID, "limit": limit},
			"count":  0,
			"output": []models.Fact{},
		})
		return nil, nil
	}

	s.mu.RLock()
	out := make([]models.Fact, 0, limit)
	for _, fact := range s.data.Facts {
		if fact.DiscordUserID != discordUserID {
			continue
		}
		if fact.Status != durableStatus {
			continue
		}
		out = append(out, fact)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeenAt.After(out[j].LastSeenAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	s.mu.RUnlock()

	s.emit(ctx, "get_durable_facts_for_discord_user", "store fetched durable facts for user", map[string]any{
		"input":  map[string]any{"discord_user_id": discordUserID, "limit": limit},
		"count":  len(out),
		"output": out,
	})
	return out, nil
}

func (s *Store) GetRecentTopicsForChannel(ctx context.Context, channelID string, recentMessageLimit, topicLimit int) ([]models.Topic, error) {
	s.mu.RLock()
	messageIDs := s.data.ChannelMessageID[channelID]
	if len(messageIDs) == 0 {
		s.mu.RUnlock()
		s.emit(ctx, "get_recent_topics_for_channel", "store fetched recent topics for channel", map[string]any{
			"input": map[string]any{
				"channel_id":           channelID,
				"recent_message_limit": recentMessageLimit,
				"topic_limit":          topicLimit,
			},
			"count":  0,
			"output": []models.Topic{},
		})
		return nil, nil
	}
	start := 0
	if len(messageIDs) > recentMessageLimit {
		start = len(messageIDs) - recentMessageLimit
	}

	seen := make(map[int64]struct{})
	topics := make([]models.Topic, 0, topicLimit)
	for _, messageID := range messageIDs[start:] {
		for topicID := range s.data.MessageTopics[messageID] {
			if _, ok := seen[topicID]; ok {
				continue
			}
			seen[topicID] = struct{}{}
			topics = append(topics, s.data.Topics[topicID])
		}
	}

	sort.Slice(topics, func(i, j int) bool {
		return topics[i].LastSeenAt.After(topics[j].LastSeenAt)
	})
	if len(topics) > topicLimit {
		topics = topics[:topicLimit]
	}
	s.mu.RUnlock()

	s.emit(ctx, "get_recent_topics_for_channel", "store fetched recent topics for channel", map[string]any{
		"input": map[string]any{
			"channel_id":           channelID,
			"recent_message_limit": recentMessageLimit,
			"topic_limit":          topicLimit,
		},
		"count":  len(topics),
		"output": topics,
	})
	return topics, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read graph store file: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}

	var decoded graphData
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("decode graph store file: %w", err)
	}
	decoded.ensureMaps()
	s.data = decoded
	return nil
}

func (s *Store) persistLocked() error {
	s.data.ensureMaps()
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal graph store data: %w", err)
	}
	if err := os.WriteFile(s.path, raw, 0o644); err != nil {
		return fmt.Errorf("write graph store file: %w", err)
	}
	return nil
}

func newGraphData() graphData {
	data := graphData{}
	data.ensureMaps()
	return data
}

func (g *graphData) ensureMaps() {
	if g.Users == nil {
		g.Users = make(map[string]userRecord)
	}
	if g.Messages == nil {
		g.Messages = make(map[string]models.Message)
	}
	if g.ChannelMessageID == nil {
		g.ChannelMessageID = make(map[string][]string)
	}
	if g.Topics == nil {
		g.Topics = make(map[int64]models.Topic)
	}
	if g.TopicNameToID == nil {
		g.TopicNameToID = make(map[string]int64)
	}
	if g.MessageTopics == nil {
		g.MessageTopics = make(map[string]map[int64]struct{})
	}
	if g.Facts == nil {
		g.Facts = make(map[int64]models.Fact)
	}
	if g.FactKeyToID == nil {
		g.FactKeyToID = make(map[string]int64)
	}
	if g.FactSources == nil {
		g.FactSources = make(map[int64]map[string]factSourceRecord)
	}
	if g.Edges == nil {
		g.Edges = make(map[int64]models.Edge)
	}
	if g.EdgeKeyToID == nil {
		g.EdgeKeyToID = make(map[string]int64)
	}
}

func sanitizeFactInput(in models.FactInput) models.FactInput {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if !isAllowedFactKind(kind) {
		kind = ""
	}

	discordUserID := normalizeDiscordUserID(in.DiscordUserID)
	aboutType := strings.ToLower(strings.TrimSpace(in.AboutType))
	aboutID := normalizeAboutID(aboutType, in.AboutID)
	value := strings.TrimSpace(in.ValueText)
	if len(value) > maxFactValueLength {
		value = strings.TrimSpace(value[:maxFactValueLength])
	}

	confidence := in.Confidence
	switch {
	case confidence < 0:
		confidence = 0
	case confidence > 1:
		confidence = 1
	}
	if confidence == 0 {
		confidence = 0.5
	}

	return models.FactInput{
		DiscordUserID: discordUserID,
		Kind:          kind,
		ValueText:     value,
		AboutType:     aboutType,
		AboutID:       aboutID,
		Confidence:    confidence,
	}
}

func normalizeAboutID(aboutType, raw string) string {
	switch aboutType {
	case "user":
		return normalizeDiscordUserID(raw)
	case "topic":
		id := strings.TrimSpace(raw)
		if id == "" {
			return ""
		}
		numeric, err := strconv.ParseInt(id, 10, 64)
		if err != nil || numeric <= 0 {
			return ""
		}
		return strconv.FormatInt(numeric, 10)
	default:
		return ""
	}
}

func sanitizeEdgeInput(in models.EdgeInput) models.EdgeInput {
	fromType := strings.ToLower(strings.TrimSpace(in.FromType))
	toType := strings.ToLower(strings.TrimSpace(in.ToType))
	edgeType := strings.ToUpper(strings.TrimSpace(in.EdgeType))
	fromID := normalizeNodeID(fromType, in.FromID)
	toID := normalizeNodeID(toType, in.ToID)

	if !isAllowedNodeType(fromType) || !isAllowedNodeType(toType) || !isAllowedEdgeType(edgeType) {
		return models.EdgeInput{}
	}
	if fromID == "" || toID == "" {
		return models.EdgeInput{}
	}

	return models.EdgeInput{
		FromType: fromType,
		FromID:   fromID,
		EdgeType: edgeType,
		ToType:   toType,
		ToID:     toID,
	}
}

func (s *Store) emit(ctx context.Context, kind, summary string, payload map[string]any) {
	if s == nil || s.telemetry == nil {
		return
	}
	s.telemetry.Emit(ctx, telemetry.StageStore, kind, summary, payload)
}

func (s *Store) emitError(ctx context.Context, kind, summary string, err error, payload map[string]any) {
	if payload == nil {
		payload = make(map[string]any, 1)
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	s.emit(ctx, kind, summary, payload)
}

func normalizeNodeID(nodeType, raw string) string {
	id := strings.TrimSpace(raw)
	switch nodeType {
	case "user":
		return normalizeDiscordUserID(id)
	case "topic", "fact":
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 {
			return ""
		}
		return strconv.FormatInt(parsed, 10)
	case "message":
		if id == "" {
			return ""
		}
		return id
	default:
		return ""
	}
}

func normalizeDiscordUserID(raw string) string {
	id := strings.TrimSpace(raw)
	if len(id) < 15 || len(id) > 21 {
		return ""
	}
	for _, ch := range id {
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	return id
}

func buildFactKey(input models.FactInput) string {
	return strings.Join([]string{
		input.DiscordUserID,
		input.Kind,
		input.AboutType,
		input.AboutID,
		cleanCanonical(input.ValueText),
	}, "|")
}

func buildEdgeKey(input models.EdgeInput) string {
	return strings.Join([]string{
		input.FromType,
		input.FromID,
		input.EdgeType,
		input.ToType,
		input.ToID,
	}, "|")
}

func cleanCanonical(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func isAllowedFactKind(kind string) bool {
	switch kind {
	case "preference", "goal", "project", "identity", "status":
		return true
	default:
		return false
	}
}

func isAllowedNodeType(nodeType string) bool {
	switch nodeType {
	case "user", "message", "topic", "fact":
		return true
	default:
		return false
	}
}

func isAllowedEdgeType(edgeType string) bool {
	switch edgeType {
	case "SENT", "MENTIONS_TOPIC", "DERIVED_FACT", "FACT_FOR_USER", "FACT_ABOUT_TOPIC":
		return true
	default:
		return false
	}
}

func normalizeTopic(topic string) string {
	topic = strings.ToLower(strings.TrimSpace(topic))
	topic = strings.Join(strings.Fields(topic), " ")
	topic = strings.TrimPrefix(topic, "the ")
	return topic
}
