package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

type Store struct {
	path      string
	mu        sync.RWMutex
	data      storeData
	telemetry *telemetry.Manager
}

type storeData struct {
	RawMessages                         map[string]models.RawMessage              `json:"raw_messages"`
	ConversationMessageIDs              map[string][]string                       `json:"conversation_message_ids"`
	NextSequenceByConversation          map[string]int64                          `json:"next_sequence_by_conversation"`
	MessageExtractions                  map[string]models.MessageExtraction       `json:"message_extractions"`
	WorkingStates                       map[string]models.WorkingState            `json:"working_states"`
	ResponseContexts                    map[string]models.ResponseContextArtifact `json:"response_contexts"`
	LatestResponseContextByConversation map[string]string                         `json:"latest_response_context_by_conversation"`
}

func NewStore(path string, manager *telemetry.Manager) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("conversation store path cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create conversation store directory: %w", err)
	}

	store := &Store{
		path:      path,
		data:      newStoreData(),
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

func (s *Store) SaveRawMessage(ctx context.Context, input models.RawMessage) (models.RawMessage, error) {
	msg := sanitizeRawMessage(input)
	if msg.MessageID == "" || msg.ConversationID == "" || msg.AuthorID == "" || msg.AuthorRole == "" {
		err := fmt.Errorf("invalid raw message after sanitization")
		s.emitError(ctx, "save_raw_message_error", "conversation store rejected raw message", err, map[string]any{
			"input":           input,
			"sanitized_input": msg,
		})
		return models.RawMessage{}, err
	}

	s.mu.Lock()
	existing, exists := s.data.RawMessages[msg.MessageID]
	if exists {
		s.mu.Unlock()
		s.emit(ctx, "save_raw_message", "conversation store saved raw message", map[string]any{
			"input":      input,
			"output":     existing,
			"created":    false,
			"message_id": existing.MessageID,
		})
		return existing, nil
	}

	nextSequence := s.data.NextSequenceByConversation[msg.ConversationID]
	if msg.SequenceNumber <= 0 {
		msg.SequenceNumber = nextSequence + 1
	}
	if msg.SequenceNumber <= nextSequence {
		s.mu.Unlock()
		err := fmt.Errorf("sequence number %d must be > %d for conversation %q", msg.SequenceNumber, nextSequence, msg.ConversationID)
		s.emitError(ctx, "save_raw_message_error", "conversation store rejected out-of-order message", err, map[string]any{
			"input":      input,
			"message_id": msg.MessageID,
		})
		return models.RawMessage{}, err
	}

	s.data.RawMessages[msg.MessageID] = msg
	s.data.ConversationMessageIDs[msg.ConversationID] = append(s.data.ConversationMessageIDs[msg.ConversationID], msg.MessageID)
	s.data.NextSequenceByConversation[msg.ConversationID] = msg.SequenceNumber
	count := len(s.data.ConversationMessageIDs[msg.ConversationID])
	s.mu.Unlock()

	s.emit(ctx, "save_raw_message", "conversation store saved raw message", map[string]any{
		"input":              input,
		"sanitized_input":    msg,
		"output":             msg,
		"created":            true,
		"conversation_count": count,
	})
	return msg, nil
}

func (s *Store) GetRawMessageByID(ctx context.Context, messageID string) (models.RawMessage, bool) {
	messageID = strings.TrimSpace(messageID)
	s.mu.RLock()
	msg, ok := s.data.RawMessages[messageID]
	s.mu.RUnlock()

	s.emit(ctx, "get_raw_message_by_id", "conversation store fetched raw message", map[string]any{
		"input":  map[string]any{"message_id": messageID},
		"output": msg,
		"found":  ok,
	})
	return msg, ok
}

func (s *Store) GetConversationMessages(ctx context.Context, conversationID string) ([]models.RawMessage, error) {
	conversationID = strings.TrimSpace(conversationID)
	s.mu.RLock()
	messageIDs := append([]string(nil), s.data.ConversationMessageIDs[conversationID]...)
	out := make([]models.RawMessage, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		out = append(out, s.data.RawMessages[messageID])
	}
	s.mu.RUnlock()

	s.emit(ctx, "get_conversation_messages", "conversation store fetched messages", map[string]any{
		"input":  map[string]any{"conversation_id": conversationID},
		"count":  len(out),
		"output": out,
	})
	return out, nil
}

func (s *Store) GetRecentRawMessages(ctx context.Context, conversationID string, limit int) ([]models.RawMessage, error) {
	if limit <= 0 {
		limit = 1
	}

	conversationID = strings.TrimSpace(conversationID)
	s.mu.RLock()
	messageIDs := s.data.ConversationMessageIDs[conversationID]
	start := 0
	if len(messageIDs) > limit {
		start = len(messageIDs) - limit
	}
	out := make([]models.RawMessage, 0, len(messageIDs)-start)
	for _, messageID := range messageIDs[start:] {
		out = append(out, s.data.RawMessages[messageID])
	}
	s.mu.RUnlock()

	s.emit(ctx, "get_recent_raw_messages", "conversation store fetched recent raw messages", map[string]any{
		"input":  map[string]any{"conversation_id": conversationID, "limit": limit},
		"count":  len(out),
		"output": out,
	})
	return out, nil
}

func (s *Store) SaveMessageExtraction(ctx context.Context, input models.MessageExtraction) (models.MessageExtraction, error) {
	extraction := sanitizeMessageExtraction(input)
	if extraction.MessageID == "" || extraction.ConversationID == "" {
		err := fmt.Errorf("invalid message extraction after sanitization")
		s.emitError(ctx, "save_message_extraction_error", "conversation store rejected message extraction", err, map[string]any{
			"input": input,
		})
		return models.MessageExtraction{}, err
	}

	s.mu.Lock()
	if _, ok := s.data.RawMessages[extraction.MessageID]; !ok {
		s.mu.Unlock()
		err := fmt.Errorf("message extraction references unknown raw message %q", extraction.MessageID)
		s.emitError(ctx, "save_message_extraction_error", "conversation store rejected message extraction", err, map[string]any{
			"input": input,
		})
		return models.MessageExtraction{}, err
	}
	s.data.MessageExtractions[extraction.MessageID] = extraction
	s.mu.Unlock()

	s.emit(ctx, "save_message_extraction", "conversation store saved message extraction", map[string]any{
		"input":  input,
		"output": extraction,
	})
	return extraction, nil
}

func (s *Store) GetMessageExtractionByID(ctx context.Context, messageID string) (models.MessageExtraction, bool) {
	messageID = strings.TrimSpace(messageID)
	s.mu.RLock()
	extraction, ok := s.data.MessageExtractions[messageID]
	s.mu.RUnlock()

	s.emit(ctx, "get_message_extraction_by_id", "conversation store fetched message extraction", map[string]any{
		"input":  map[string]any{"message_id": messageID},
		"output": extraction,
		"found":  ok,
	})
	return extraction, ok
}

func (s *Store) GetRecentMessageExtractions(ctx context.Context, conversationID string, limit int) ([]models.MessageExtraction, error) {
	if limit <= 0 {
		limit = 1
	}

	conversationID = strings.TrimSpace(conversationID)
	s.mu.RLock()
	messageIDs := s.data.ConversationMessageIDs[conversationID]
	start := 0
	if len(messageIDs) > limit {
		start = len(messageIDs) - limit
	}
	out := make([]models.MessageExtraction, 0, len(messageIDs)-start)
	for _, messageID := range messageIDs[start:] {
		extraction, ok := s.data.MessageExtractions[messageID]
		if !ok {
			continue
		}
		out = append(out, extraction)
	}
	s.mu.RUnlock()

	s.emit(ctx, "get_recent_message_extractions", "conversation store fetched recent message extractions", map[string]any{
		"input":  map[string]any{"conversation_id": conversationID, "limit": limit},
		"count":  len(out),
		"output": out,
	})
	return out, nil
}

func (s *Store) SaveWorkingState(ctx context.Context, input models.WorkingState) (models.WorkingState, error) {
	state := sanitizeWorkingState(input)
	if state.ConversationID == "" {
		err := fmt.Errorf("working state conversation id cannot be empty")
		s.emitError(ctx, "save_working_state_error", "conversation store rejected working state", err, map[string]any{
			"input": input,
		})
		return models.WorkingState{}, err
	}

	s.mu.Lock()
	s.data.WorkingStates[state.ConversationID] = state
	s.mu.Unlock()

	s.emit(ctx, "save_working_state", "conversation store saved working state", map[string]any{
		"input":  input,
		"output": state,
	})
	return state, nil
}

func (s *Store) GetWorkingState(ctx context.Context, conversationID string) (models.WorkingState, bool) {
	conversationID = strings.TrimSpace(conversationID)
	s.mu.RLock()
	state, ok := s.data.WorkingStates[conversationID]
	s.mu.RUnlock()

	s.emit(ctx, "get_working_state", "conversation store fetched working state", map[string]any{
		"input":  map[string]any{"conversation_id": conversationID},
		"output": state,
		"found":  ok,
	})
	return state, ok
}

func (s *Store) SaveResponseContext(ctx context.Context, input models.ResponseContextArtifact) (models.ResponseContextArtifact, error) {
	artifact := sanitizeResponseContextArtifact(input)
	if artifact.MessageID == "" || artifact.ConversationID == "" || artifact.Brief == "" {
		err := fmt.Errorf("invalid response context artifact after sanitization")
		s.emitError(ctx, "save_response_context_error", "conversation store rejected response context artifact", err, map[string]any{
			"input": input,
		})
		return models.ResponseContextArtifact{}, err
	}

	s.mu.Lock()
	s.data.ResponseContexts[artifact.MessageID] = artifact
	s.data.LatestResponseContextByConversation[artifact.ConversationID] = artifact.MessageID
	s.mu.Unlock()

	s.emit(ctx, "save_response_context", "conversation store saved response context artifact", map[string]any{
		"input":  input,
		"output": artifact,
	})
	return artifact, nil
}

func (s *Store) GetLatestResponseContext(ctx context.Context, conversationID string) (models.ResponseContextArtifact, bool) {
	conversationID = strings.TrimSpace(conversationID)
	s.mu.RLock()
	messageID := s.data.LatestResponseContextByConversation[conversationID]
	artifact, ok := s.data.ResponseContexts[messageID]
	s.mu.RUnlock()

	s.emit(ctx, "get_latest_response_context", "conversation store fetched latest response context artifact", map[string]any{
		"input":  map[string]any{"conversation_id": conversationID},
		"output": artifact,
		"found":  ok,
	})
	return artifact, ok
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read conversation store file: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}

	var decoded storeData
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("decode conversation store file: %w", err)
	}
	decoded.ensureMaps()
	s.data = decoded
	return nil
}

func (s *Store) persistLocked() error {
	s.data.ensureMaps()
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conversation store data: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(s.path, raw, 0o644); err != nil {
		return fmt.Errorf("write conversation store file: %w", err)
	}
	return nil
}

func newStoreData() storeData {
	data := storeData{}
	data.ensureMaps()
	return data
}

func (d *storeData) ensureMaps() {
	if d.RawMessages == nil {
		d.RawMessages = make(map[string]models.RawMessage)
	}
	if d.ConversationMessageIDs == nil {
		d.ConversationMessageIDs = make(map[string][]string)
	}
	if d.NextSequenceByConversation == nil {
		d.NextSequenceByConversation = make(map[string]int64)
	}
	if d.MessageExtractions == nil {
		d.MessageExtractions = make(map[string]models.MessageExtraction)
	}
	if d.WorkingStates == nil {
		d.WorkingStates = make(map[string]models.WorkingState)
	}
	if d.ResponseContexts == nil {
		d.ResponseContexts = make(map[string]models.ResponseContextArtifact)
	}
	if d.LatestResponseContextByConversation == nil {
		d.LatestResponseContextByConversation = make(map[string]string)
	}
}

func sanitizeRawMessage(input models.RawMessage) models.RawMessage {
	role := strings.ToLower(strings.TrimSpace(input.AuthorRole))
	switch role {
	case "user", "assistant":
	default:
		role = ""
	}

	return models.RawMessage{
		MessageID:        strings.TrimSpace(input.MessageID),
		ConversationID:   strings.TrimSpace(input.ConversationID),
		SequenceNumber:   input.SequenceNumber,
		AuthorID:         strings.TrimSpace(input.AuthorID),
		AuthorRole:       role,
		Content:          strings.TrimSpace(input.Content),
		TimestampUnixMs:  input.TimestampUnixMs,
		ReplyToMessageID: strings.TrimSpace(input.ReplyToMessageID),
	}
}

func sanitizeMessageExtraction(input models.MessageExtraction) models.MessageExtraction {
	output := input
	output.MessageID = strings.TrimSpace(output.MessageID)
	output.ConversationID = strings.TrimSpace(output.ConversationID)
	output.MessageSummary = strings.TrimSpace(output.MessageSummary)
	output.ClaimsStatus = normalizeStatus(output.ClaimsStatus)
	output.QuestionsStatus = normalizeStatus(output.QuestionsStatus)
	output.TopicsStatus = normalizeStatus(output.TopicsStatus)
	output.PronounsStatus = normalizeStatus(output.PronounsStatus)
	output.SummaryStatus = normalizeStatus(output.SummaryStatus)
	output.ClaimsModelVersion = strings.TrimSpace(output.ClaimsModelVersion)
	output.QuestionsModelVersion = strings.TrimSpace(output.QuestionsModelVersion)
	output.TopicsModelVersion = strings.TrimSpace(output.TopicsModelVersion)
	output.PronounsModelVersion = strings.TrimSpace(output.PronounsModelVersion)
	output.SummaryModelVersion = strings.TrimSpace(output.SummaryModelVersion)
	output.RawClaimsOutput = strings.TrimSpace(output.RawClaimsOutput)
	output.RawQuestionsOutput = strings.TrimSpace(output.RawQuestionsOutput)
	output.RawTopicsOutput = strings.TrimSpace(output.RawTopicsOutput)
	output.RawPronounsOutput = strings.TrimSpace(output.RawPronounsOutput)
	output.RawSummaryOutput = strings.TrimSpace(output.RawSummaryOutput)
	return output
}

func sanitizeWorkingState(input models.WorkingState) models.WorkingState {
	output := input
	output.ConversationID = strings.TrimSpace(output.ConversationID)
	output.LastUpdatedMessageID = strings.TrimSpace(output.LastUpdatedMessageID)
	output.RollingSummary = strings.TrimSpace(output.RollingSummary)
	output.LastCompactedAtMessage = strings.TrimSpace(output.LastCompactedAtMessage)
	output.RecentMessageIDs = trimAndDedupeStrings(output.RecentMessageIDs)
	output.RecentExtractionIDs = trimAndDedupeStrings(output.RecentExtractionIDs)
	return output
}

func sanitizeResponseContextArtifact(input models.ResponseContextArtifact) models.ResponseContextArtifact {
	output := input
	output.MessageID = strings.TrimSpace(output.MessageID)
	output.ConversationID = strings.TrimSpace(output.ConversationID)
	output.Brief = strings.TrimSpace(output.Brief)
	output.RecentMessageIDs = trimAndDedupeStrings(output.RecentMessageIDs)
	return output
}

func normalizeStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ok":
		return "ok"
	case "partial":
		return "partial"
	case "failed":
		return "failed"
	default:
		return "failed"
	}
}

func trimAndDedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
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
