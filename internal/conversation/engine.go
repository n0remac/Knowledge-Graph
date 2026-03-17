package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

const (
	telemetryKindRequest  = "ollama_request"
	telemetryKindResponse = "ollama_response"
)

type Engine struct {
	store          *Store
	client         *ollama.Client
	model          string
	requestTimeout time.Duration
	telemetry      *telemetry.Manager
}

type ProcessOptions struct {
	ReplyTarget *models.RawMessage
}

type ProcessResult struct {
	Message             models.RawMessage
	Extraction          models.MessageExtraction
	WorkingState        models.WorkingState
	ResponseContext     models.ResponseContextArtifact
	SummaryUpdateStatus string
}

func NewEngine(store *Store, client *ollama.Client, model string, requestTimeout time.Duration, manager *telemetry.Manager) *Engine {
	return &Engine{
		store:          store,
		client:         client,
		model:          strings.TrimSpace(model),
		requestTimeout: requestTimeout,
		telemetry:      manager,
	}
}

func (e *Engine) ProcessMessage(ctx context.Context, input models.RawMessage, options ProcessOptions) (ProcessResult, error) {
	if e == nil || e.store == nil {
		return ProcessResult{}, fmt.Errorf("conversation engine is not initialized")
	}

	message, err := e.store.SaveRawMessage(ctx, input)
	if err != nil {
		return ProcessResult{}, err
	}

	replyTarget := e.resolveReplyTarget(message, options)
	previousState, ok := e.store.GetWorkingState(ctx, message.ConversationID)
	if !ok {
		previousState = models.WorkingState{ConversationID: message.ConversationID}
	}

	recentMessages, err := e.store.GetRecentRawMessages(ctx, message.ConversationID, maxPromptRecentMessages+1)
	if err != nil {
		return ProcessResult{}, err
	}
	envelope := promptEnvelope{
		CurrentMessage: message,
		ReplyTarget:    replyTarget,
		RecentMessages: promptRecentMessages(recentMessages, message.MessageID),
		WorkingState:   previousState,
	}

	e.emit(ctx, "parallel_extraction_started", "parallel extraction batch started", map[string]any{
		"message": map[string]any{
			"message_id":      message.MessageID,
			"conversation_id": message.ConversationID,
			"sequence_number": message.SequenceNumber,
		},
		"extractors": []string{"claims", "questions", "topics", "pronouns", "summary"},
	})

	extraction := e.runParallelExtractions(ctx, envelope)
	e.emit(ctx, "parallel_extraction_completed", "parallel extraction batch completed", map[string]any{
		"message":            messagePayload(message),
		"extractor_statuses": extractorStatusesPayload(extraction),
		"counts":             statusCounts(extraction),
		"message_extraction": messageExtractionPayload(extraction),
	})

	savedExtraction, err := e.store.SaveMessageExtraction(ctx, extraction)
	if err != nil {
		return ProcessResult{}, err
	}
	e.emit(ctx, "message_extraction_persisted", "message extraction persisted", map[string]any{
		"message":            messagePayload(message),
		"extractor_statuses": extractorStatusesPayload(savedExtraction),
		"message_extraction": messageExtractionPayload(savedExtraction),
	})

	summaryResult := e.updateSummaryWithTimeout(ctx, envelope, savedExtraction)
	rollingSummary := previousState.RollingSummary
	if strings.TrimSpace(summaryResult.Summary) != "" && summaryResult.Status != "failed" {
		rollingSummary = summaryResult.Summary
	}

	mergedState := mergeWorkingState(previousState, message, savedExtraction, rollingSummary, e.sequenceForMessage)
	savedState, err := e.store.SaveWorkingState(ctx, mergedState)
	if err != nil {
		return ProcessResult{}, err
	}
	e.emit(ctx, "working_state_updated", "working state updated", map[string]any{
		"message":               messagePayload(message),
		"summary_update_status": summaryResult.Status,
		"working_state":         workingStatePayload(savedState),
		"counts":                formatWorkingStateCounts(savedState),
		"summary_preview":       summaryPreview(savedState.RollingSummary, 180),
	})

	contextMessages, err := e.store.GetRecentRawMessages(ctx, message.ConversationID, maxResponseRecentMessages)
	if err != nil {
		return ProcessResult{}, err
	}
	artifact := buildResponseContextArtifact(message, savedState, contextMessages)
	savedArtifact, err := e.store.SaveResponseContext(ctx, artifact)
	if err != nil {
		return ProcessResult{}, err
	}
	e.emit(ctx, "response_context_built", "response context artifact built", map[string]any{
		"message":          messagePayload(message),
		"response_context": responseContextPayload(savedArtifact),
	})

	return ProcessResult{
		Message:             message,
		Extraction:          savedExtraction,
		WorkingState:        savedState,
		ResponseContext:     savedArtifact,
		SummaryUpdateStatus: summaryResult.Status,
	}, nil
}

func (e *Engine) RebuildConversationState(ctx context.Context, conversationID string) (models.WorkingState, error) {
	if e == nil || e.store == nil {
		return models.WorkingState{}, fmt.Errorf("conversation engine is not initialized")
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return models.WorkingState{}, fmt.Errorf("conversation id cannot be empty")
	}

	messages, err := e.store.GetConversationMessages(ctx, conversationID)
	if err != nil {
		return models.WorkingState{}, err
	}
	if len(messages) == 0 {
		return models.WorkingState{}, fmt.Errorf("conversation %q has no messages", conversationID)
	}

	state := models.WorkingState{ConversationID: conversationID}
	for i, message := range messages {
		extraction, _ := e.store.GetMessageExtractionByID(ctx, message.MessageID)
		replyTarget := e.resolveReplyTarget(message, ProcessOptions{})
		envelope := promptEnvelope{
			CurrentMessage: message,
			ReplyTarget:    replyTarget,
			RecentMessages: promptRecentMessages(messages[max(0, i-maxPromptRecentMessages):i], ""),
			WorkingState:   state,
		}
		summaryResult := e.updateSummaryWithTimeout(ctx, envelope, extraction)
		rollingSummary := state.RollingSummary
		if strings.TrimSpace(summaryResult.Summary) != "" && summaryResult.Status != "failed" {
			rollingSummary = summaryResult.Summary
		}
		state = mergeWorkingState(state, message, extraction, rollingSummary, e.sequenceForMessage)
	}

	savedState, err := e.store.SaveWorkingState(ctx, state)
	if err != nil {
		return models.WorkingState{}, err
	}
	recentMessages, err := e.store.GetRecentRawMessages(ctx, conversationID, maxResponseRecentMessages)
	if err != nil {
		return models.WorkingState{}, err
	}
	artifact := buildResponseContextArtifact(messages[len(messages)-1], savedState, recentMessages)
	if _, err := e.store.SaveResponseContext(ctx, artifact); err != nil {
		return models.WorkingState{}, err
	}

	e.emit(ctx, "working_state_rebuilt", "working state rebuilt from stored messages and extractions", map[string]any{
		"conversation_id": conversationID,
		"working_state":   workingStatePayload(savedState),
		"message_count":   len(messages),
	})
	return savedState, nil
}

func (e *Engine) runParallelExtractions(ctx context.Context, envelope promptEnvelope) models.MessageExtraction {
	result := models.MessageExtraction{
		MessageID:       envelope.CurrentMessage.MessageID,
		ConversationID:  envelope.CurrentMessage.ConversationID,
		ClaimsStatus:    "failed",
		QuestionsStatus: "failed",
		TopicsStatus:    "failed",
		PronounsStatus:  "failed",
		SummaryStatus:   "failed",
	}

	calls := []struct {
		name string
		run  func(context.Context, promptEnvelope) extractionDimension
	}{
		{name: "claims", run: func(callCtx context.Context, prompt promptEnvelope) extractionDimension {
			return e.extractClaims(callCtx, prompt)
		}},
		{name: "questions", run: func(callCtx context.Context, prompt promptEnvelope) extractionDimension {
			return e.extractQuestions(callCtx, prompt)
		}},
		{name: "topics", run: func(callCtx context.Context, prompt promptEnvelope) extractionDimension {
			return e.extractTopics(callCtx, prompt)
		}},
		{name: "pronouns", run: func(callCtx context.Context, prompt promptEnvelope) extractionDimension {
			return e.extractPronouns(callCtx, prompt)
		}},
		{name: "summary", run: func(callCtx context.Context, prompt promptEnvelope) extractionDimension {
			return e.summarizeMessage(callCtx, prompt)
		}},
	}

	callCtx, cancel := context.WithTimeout(ctx, e.batchTimeout())
	defer cancel()

	resultsCh := make(chan extractionDimension, len(calls))
	for _, call := range calls {
		call := call
		go func() {
			resultsCh <- call.run(callCtx, envelope)
		}()
	}

	collected := make(map[string]extractionDimension, len(calls))
	for len(collected) < len(calls) {
		select {
		case res := <-resultsCh:
			collected[res.Name] = res
		case <-callCtx.Done():
			for _, call := range calls {
				if _, ok := collected[call.name]; ok {
					continue
				}
				collected[call.name] = extractionDimension{
					Name:   call.name,
					Status: "failed",
					Model:  e.model,
					Err:    callCtx.Err(),
				}
			}
		}
	}

	for _, res := range collected {
		switch res.Name {
		case "claims":
			result.Claims = res.Claims
			result.ClaimsStatus = fallbackStatus(res.Status)
			result.ClaimsModelVersion = firstNonEmpty(res.Model, e.model)
			result.RawClaimsOutput = res.Raw
		case "questions":
			result.OpenQuestions = res.Questions
			result.QuestionsStatus = fallbackStatus(res.Status)
			result.QuestionsModelVersion = firstNonEmpty(res.Model, e.model)
			result.RawQuestionsOutput = res.Raw
		case "topics":
			result.ActiveTopics = res.Topics
			result.TopicsStatus = fallbackStatus(res.Status)
			result.TopicsModelVersion = firstNonEmpty(res.Model, e.model)
			result.RawTopicsOutput = res.Raw
		case "pronouns":
			result.PronounResolutions = res.Pronouns
			result.PronounsStatus = fallbackStatus(res.Status)
			result.PronounsModelVersion = firstNonEmpty(res.Model, e.model)
			result.RawPronounsOutput = res.Raw
		case "summary":
			result.MessageSummary = res.Summary
			result.SummaryStatus = fallbackStatus(res.Status)
			result.SummaryModelVersion = firstNonEmpty(res.Model, e.model)
			result.RawSummaryOutput = res.Raw
		}
	}
	return result
}

func (e *Engine) updateSummaryWithTimeout(ctx context.Context, envelope promptEnvelope, extraction models.MessageExtraction) summaryUpdateResult {
	callCtx, cancel := context.WithTimeout(ctx, e.summaryTimeout())
	defer cancel()
	result := e.updateRollingSummary(callCtx, envelope, extraction)
	if result.Status == "" {
		result.Status = "failed"
	}
	return result
}

func (e *Engine) batchTimeout() time.Duration {
	defaultTimeout := minDuration(4*time.Second, e.requestTimeout/2)
	if defaultTimeout <= 0 {
		return 4 * time.Second
	}
	return defaultTimeout
}

func (e *Engine) summaryTimeout() time.Duration {
	defaultTimeout := minDuration(2500*time.Millisecond, e.requestTimeout/4)
	if defaultTimeout <= 0 {
		return 2500 * time.Millisecond
	}
	return defaultTimeout
}

func (e *Engine) resolveReplyTarget(message models.RawMessage, options ProcessOptions) *models.RawMessage {
	if options.ReplyTarget != nil && strings.TrimSpace(options.ReplyTarget.Content) != "" {
		reply := sanitizeRawMessage(*options.ReplyTarget)
		return &reply
	}
	if strings.TrimSpace(message.ReplyToMessageID) == "" {
		return nil
	}
	e.store.mu.RLock()
	defer e.store.mu.RUnlock()
	reply, ok := e.store.data.RawMessages[message.ReplyToMessageID]
	if !ok || strings.TrimSpace(reply.Content) == "" {
		return nil
	}
	copied := reply
	return &copied
}

func (e *Engine) sequenceForMessage(messageID string) int64 {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || e == nil || e.store == nil {
		return 0
	}
	e.store.mu.RLock()
	defer e.store.mu.RUnlock()
	if message, ok := e.store.data.RawMessages[messageID]; ok {
		return message.SequenceNumber
	}
	return 0
}

func promptRecentMessages(messages []models.RawMessage, currentMessageID string) []models.RawMessage {
	out := make([]models.RawMessage, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.MessageID) == "" || message.MessageID == currentMessageID {
			continue
		}
		out = append(out, message)
	}
	if len(out) > maxPromptRecentMessages {
		out = out[len(out)-maxPromptRecentMessages:]
	}
	return out
}

func fallbackStatus(value string) string {
	switch value {
	case "ok", "partial", "failed":
		return value
	default:
		return "failed"
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (e *Engine) emit(ctx context.Context, kind, summary string, payload map[string]any) {
	if e == nil || e.telemetry == nil {
		return
	}
	e.telemetry.Emit(ctx, telemetry.StageConversation, kind, summary, payload)
}
