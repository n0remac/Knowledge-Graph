package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

const (
	durableConfidenceThreshold = 0.85
	candidateStatus            = "candidate"
	durableStatus              = "durable"
)

type Store struct {
	path string
	mu   sync.RWMutex
	data graphData
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
	NextTopicID      int64                                 `json:"next_topic_id"`
	NextFactID       int64                                 `json:"next_fact_id"`
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

func NewGraph(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("graph store path cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create graph store directory: %w", err)
	}

	store := &Store{
		path: path,
		data: newGraphData(),
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

func (s *Store) UpsertUser(_ context.Context, id, username, displayName string, now time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("user id cannot be empty")
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
	defer s.mu.Unlock()
	s.data.Users[id] = userRecord{
		ID:          id,
		Username:    username,
		DisplayName: displayName,
		UpdatedAt:   now.UTC().UnixMilli(),
	}
	return nil
}

func (s *Store) SaveMessage(_ context.Context, msg models.Message) error {
	if msg.ID == "" {
		return fmt.Errorf("message id cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.data.Messages[msg.ID]; exists {
		return nil
	}

	s.data.Messages[msg.ID] = msg
	s.data.ChannelMessageID[msg.ChannelID] = append(s.data.ChannelMessageID[msg.ChannelID], msg.ID)
	return nil
}

func (s *Store) UpsertTopic(_ context.Context, name string, seenAt time.Time) (models.Topic, error) {
	normalized := normalizeTopic(name)
	if normalized == "" {
		return models.Topic{}, fmt.Errorf("topic name is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.data.TopicNameToID[normalized]; ok {
		topic := s.data.Topics[id]
		if seenAt.After(topic.LastSeenAt) {
			topic.LastSeenAt = seenAt.UTC()
			s.data.Topics[id] = topic
		}
		return topic, nil
	}

	s.data.NextTopicID++
	topic := models.Topic{
		ID:         s.data.NextTopicID,
		Name:       normalized,
		Summary:    "",
		LastSeenAt: seenAt.UTC(),
	}
	s.data.Topics[topic.ID] = topic
	s.data.TopicNameToID[normalized] = topic.ID
	return topic, nil
}

func (s *Store) LinkMessageTopic(_ context.Context, messageID string, topicID int64) error {
	if strings.TrimSpace(messageID) == "" || topicID <= 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Messages[messageID]; !ok {
		return fmt.Errorf("cannot link unknown message %q", messageID)
	}
	if _, ok := s.data.Topics[topicID]; !ok {
		return fmt.Errorf("cannot link unknown topic %d", topicID)
	}

	if s.data.MessageTopics[messageID] == nil {
		s.data.MessageTopics[messageID] = make(map[int64]struct{})
	}
	s.data.MessageTopics[messageID][topicID] = struct{}{}
	return nil
}

func (s *Store) UpsertFactFromMessage(_ context.Context, input models.FactInput, messageID, modelName string, observedAt time.Time) (models.Fact, error) {
	clean := sanitizeFactInput(input)
	if clean.Kind == "" || clean.SubjectID == "" || clean.ValueText == "" {
		return models.Fact{}, fmt.Errorf("invalid fact input after sanitization")
	}
	if strings.TrimSpace(messageID) == "" {
		return models.Fact{}, fmt.Errorf("message id cannot be empty for fact source")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	factKey := buildFactKey(clean)
	factID, exists := s.data.FactKeyToID[factKey]
	now := observedAt.UTC()
	if !exists {
		s.data.NextFactID++
		factID = s.data.NextFactID
		status := candidateStatus
		if clean.Confidence >= durableConfidenceThreshold {
			status = durableStatus
		}
		s.data.Facts[factID] = models.Fact{
			ID:         factID,
			Kind:       clean.Kind,
			SubjectID:  clean.SubjectID,
			ObjectID:   clean.ObjectID,
			ValueText:  clean.ValueText,
			Confidence: clean.Confidence,
			Status:     status,
			CreatedAt:  now,
			LastSeenAt: now,
		}
		s.data.FactKeyToID[factKey] = factID
	} else {
		fact := s.data.Facts[factID]
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

	return s.data.Facts[factID], nil
}

func (s *Store) GetRecentMessages(_ context.Context, channelID string, limit int) ([]models.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messageIDs := s.data.ChannelMessageID[channelID]
	if len(messageIDs) == 0 {
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
	return out, nil
}

func (s *Store) GetFactsForSubjects(_ context.Context, subjectIDs []string, limit int) ([]models.Fact, error) {
	subjectSet := make(map[string]struct{}, len(subjectIDs))
	for _, id := range subjectIDs {
		if id = strings.TrimSpace(id); id != "" {
			subjectSet[id] = struct{}{}
		}
	}
	if len(subjectSet) == 0 {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]models.Fact, 0, limit)
	for _, fact := range s.data.Facts {
		if _, ok := subjectSet[fact.SubjectID]; !ok {
			continue
		}
		out = append(out, fact)
	}

	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Status != right.Status {
			return left.Status == durableStatus
		}
		return left.LastSeenAt.After(right.LastSeenAt)
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) GetRecentTopicsForChannel(_ context.Context, channelID string, recentMessageLimit, topicLimit int) ([]models.Topic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messageIDs := s.data.ChannelMessageID[channelID]
	if len(messageIDs) == 0 {
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
}

func sanitizeFactInput(in models.FactInput) models.FactInput {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if !isAllowedFactKind(kind) {
		kind = ""
	}

	subject := strings.TrimSpace(in.SubjectID)
	object := strings.TrimSpace(in.ObjectID)
	value := strings.TrimSpace(in.ValueText)

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
		Kind:       kind,
		SubjectID:  subject,
		ObjectID:   object,
		ValueText:  value,
		Confidence: confidence,
	}
}

func buildFactKey(input models.FactInput) string {
	return strings.Join([]string{
		input.Kind,
		input.SubjectID,
		input.ObjectID,
		cleanCanonical(input.ValueText),
	}, "|")
}

func cleanCanonical(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func isAllowedFactKind(kind string) bool {
	switch kind {
	case "preference", "goal", "project", "relationship", "identity", "status":
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
