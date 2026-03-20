package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/claimextract"
	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
)

type promptEnvelope struct {
	CurrentMessage models.RawMessage
	ReplyTarget    *models.RawMessage
	RecentMessages []models.RawMessage
	WorkingState   models.WorkingState
}

type extractionDimension struct {
	Name    string
	Status  string
	Raw     string
	Model   string
	Err     error
	Claims  []models.Claim
	Topics  []models.TopicRef
	Summary string
}

type summaryUpdateResult struct {
	Status  string
	Raw     string
	Model   string
	Err     error
	Summary string
}

type topicsPayload struct {
	Topics []string `json:"topics"`
}

type summaryPayload struct {
	Summary string `json:"summary"`
}

type rollingSummaryPayload struct {
	RollingSummary string `json:"rolling_summary"`
}

func (e *Engine) extractClaims(ctx context.Context, envelope promptEnvelope) extractionDimension {
	result := e.claims.Extract(ctx, claimextract.Input{
		CurrentMessage: envelope.CurrentMessage,
		ReplyTarget:    envelope.ReplyTarget,
		RecentMessages: envelope.RecentMessages,
		WorkingState:   envelope.WorkingState,
	})
	if result.Err != nil {
		return extractionDimension{Name: "claims", Status: "failed", Raw: result.Raw, Model: result.Model, Err: result.Err}
	}
	return extractionDimension{Name: "claims", Status: result.Status, Raw: result.Raw, Model: result.Model, Claims: result.Claims}
}

func (e *Engine) extractTopics(ctx context.Context, envelope promptEnvelope) extractionDimension {
	systemPrompt := `You extract active conversational topics.
Return ONLY valid JSON with this exact schema:
{
  "topics": ["topic"]
}

Rules:
- Return short normalized topic names.
- Prefer 1 to 4 topics.
- Keep topics at the level of current conversation threads, not generic domains.
- If there are no meaningful active topics, return {"topics":[]}.`

	userPrompt := buildSharedPromptEnvelope(envelope)
	raw, model, err := e.callLLM(ctx, "extract_topics", systemPrompt, userPrompt)
	if err != nil {
		return extractionDimension{Name: "topics", Status: "failed", Raw: raw, Model: model, Err: err}
	}

	topics, status, parseErr := parseTopicsPayload(raw)
	if parseErr != nil {
		return extractionDimension{Name: "topics", Status: "failed", Raw: raw, Model: model, Err: parseErr}
	}
	return extractionDimension{Name: "topics", Status: status, Raw: raw, Model: model, Topics: topics}
}

func (e *Engine) summarizeMessage(ctx context.Context, envelope promptEnvelope) extractionDimension {
	systemPrompt := `You summarize a single message for short-term conversational memory.
Return ONLY valid JSON with this exact schema:
{
  "summary": "one or two short sentences"
}

Rules:
- Focus on what this message contributes to the active conversation.
- Keep it compact and plain-language.
- If the message contributes very little, still summarize it briefly.`

	userPrompt := buildSharedPromptEnvelope(envelope)
	raw, model, err := e.callLLM(ctx, "summarize_message", systemPrompt, userPrompt)
	if err != nil {
		return extractionDimension{Name: "summary", Status: "failed", Raw: raw, Model: model, Err: err}
	}

	summary, status, parseErr := parseSummaryPayload(raw)
	if parseErr != nil {
		return extractionDimension{Name: "summary", Status: "failed", Raw: raw, Model: model, Err: parseErr}
	}
	return extractionDimension{Name: "summary", Status: status, Raw: raw, Model: model, Summary: summary}
}

func (e *Engine) updateRollingSummary(ctx context.Context, envelope promptEnvelope, extraction models.MessageExtraction) summaryUpdateResult {
	systemPrompt := `You update a rolling summary of the active conversation.
Return ONLY valid JSON with this exact schema:
{
  "rolling_summary": "compact summary"
}

Rules:
- Keep the summary readable, compact, and focused on the current direction.
- Favor recent developments, important claims, and active topics.
- Do not restate the entire conversation.
- Use the current raw message and recent evidence, not just the previous summary.`

	userPrompt := buildRollingSummaryPrompt(envelope, extraction)
	raw, model, err := e.callLLM(ctx, "update_rolling_summary", systemPrompt, userPrompt)
	if err != nil {
		return summaryUpdateResult{Status: "failed", Raw: raw, Model: model, Err: err}
	}

	summary, status, parseErr := parseRollingSummaryPayload(raw)
	if parseErr != nil {
		return summaryUpdateResult{Status: "failed", Raw: raw, Model: model, Err: parseErr}
	}
	return summaryUpdateResult{Status: status, Raw: raw, Model: model, Summary: summary}
}

func (e *Engine) callLLM(ctx context.Context, purpose, systemPrompt, userPrompt string) (string, string, error) {
	messages := []ollama.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	request := ollama.ChatRequest{
		Model:    e.model,
		Messages: messages,
		Stream:   false,
	}
	requestBody, _ := json.Marshal(request)
	e.emit(ctx, telemetryKindRequest, purpose+" request prepared", map[string]any{
		"purpose":           purpose,
		"model":             e.model,
		"messages":          messages,
		"system_prompt":     systemPrompt,
		"user_prompt":       userPrompt,
		"request_body_json": string(requestBody),
	})

	result, err := e.client.ChatDetailed(ctx, request)
	if err != nil {
		e.emit(ctx, telemetryKindResponse, purpose+" request failed", map[string]any{
			"purpose":           purpose,
			"model":             e.model,
			"request_body_json": string(requestBody),
			"raw_response":      result.RawResponse,
			"error":             err.Error(),
		})
		return strings.TrimSpace(result.ResponseContent), e.model, err
	}

	e.emit(ctx, telemetryKindResponse, purpose+" response received", map[string]any{
		"purpose":      purpose,
		"model":        e.model,
		"raw_response": result.RawResponse,
	})
	return strings.TrimSpace(result.ResponseContent), e.model, nil
}

func buildSharedPromptEnvelope(envelope promptEnvelope) string {
	var builder strings.Builder
	builder.WriteString("current_message:\n")
	builder.WriteString(envelope.CurrentMessage.Content)
	builder.WriteString("\n\nauthor_role:\n")
	builder.WriteString(envelope.CurrentMessage.AuthorRole)

	builder.WriteString("\n\nreply_target:\n")
	if envelope.ReplyTarget == nil || strings.TrimSpace(envelope.ReplyTarget.Content) == "" {
		builder.WriteString("none")
	} else {
		builder.WriteString(envelope.ReplyTarget.Content)
	}

	builder.WriteString("\n\nrecent_messages:\n")
	builder.WriteString(formatRawMessages(envelope.RecentMessages))

	builder.WriteString("\ncurrent_rolling_summary:\n")
	if strings.TrimSpace(envelope.WorkingState.RollingSummary) == "" {
		builder.WriteString("none")
	} else {
		builder.WriteString(envelope.WorkingState.RollingSummary)
	}

	builder.WriteString("\n\ncurrent_active_topics:\n")
	if len(envelope.WorkingState.ActiveTopics) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, topic := range envelope.WorkingState.ActiveTopics {
			builder.WriteString("- ")
			builder.WriteString(topic.Name)
			builder.WriteString(" (status=")
			builder.WriteString(topic.Status)
			builder.WriteString(" salience=")
			builder.WriteString(fmt.Sprintf("%.2f", topic.Salience))
			builder.WriteString(")\n")
		}
	}
	return builder.String()
}

func buildRollingSummaryPrompt(envelope promptEnvelope, extraction models.MessageExtraction) string {
	var builder strings.Builder
	builder.WriteString(buildSharedPromptEnvelope(envelope))

	builder.WriteString("\n\nmessage_artifacts:\n")
	builder.WriteString("message_summary:\n")
	if strings.TrimSpace(extraction.MessageSummary) == "" {
		builder.WriteString("none\n")
	} else {
		builder.WriteString(extraction.MessageSummary)
		builder.WriteString("\n")
	}

	builder.WriteString("claims:\n")
	if len(extraction.Claims) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, claim := range extraction.Claims {
			builder.WriteString("- ")
			builder.WriteString(claim.Subject)
			builder.WriteString(" | ")
			builder.WriteString(claim.Predicate)
			builder.WriteString(" | ")
			builder.WriteString(claim.Object)
			builder.WriteString("\n")
		}
	}

	builder.WriteString("topics:\n")
	if len(extraction.ActiveTopics) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, topic := range extraction.ActiveTopics {
			builder.WriteString("- ")
			builder.WriteString(topic.Name)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func formatRawMessages(messages []models.RawMessage) string {
	if len(messages) == 0 {
		return "- none\n"
	}

	var builder strings.Builder
	for _, message := range messages {
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		builder.WriteString("- ")
		builder.WriteString(message.AuthorRole)
		builder.WriteString(":")
		builder.WriteString(message.AuthorID)
		builder.WriteString(": ")
		builder.WriteString(message.Content)
		builder.WriteString("\n")
	}
	if builder.Len() == 0 {
		return "- none\n"
	}
	return builder.String()
}

func parseTopicsPayload(raw string) ([]models.TopicRef, string, error) {
	body, err := extractJSONObject(raw)
	if err != nil {
		return nil, "", err
	}
	var payload topicsPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, "", err
	}

	seen := make(map[string]struct{}, len(payload.Topics))
	out := make([]models.TopicRef, 0, len(payload.Topics))
	dropped := 0
	for _, topic := range payload.Topics {
		for _, part := range splitTopicCandidate(topic) {
			name := normalizeTopicName(part)
			if name == "" {
				dropped++
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, models.TopicRef{Name: name})
		}
	}
	return out, payloadStatus(len(out), dropped), nil
}

func parseSummaryPayload(raw string) (string, string, error) {
	body, err := extractJSONObject(raw)
	if err == nil {
		var payload summaryPayload
		if jsonErr := json.Unmarshal([]byte(body), &payload); jsonErr == nil {
			summary := strings.TrimSpace(payload.Summary)
			if summary != "" {
				return summary, "ok", nil
			}
		}
	}

	fallback := trimmedPlainText(raw)
	if fallback == "" {
		if err != nil {
			return "", "", err
		}
		return "", "", fmt.Errorf("summary payload empty")
	}
	return fallback, "partial", nil
}

func parseRollingSummaryPayload(raw string) (string, string, error) {
	body, err := extractJSONObject(raw)
	if err == nil {
		var payload rollingSummaryPayload
		if jsonErr := json.Unmarshal([]byte(body), &payload); jsonErr == nil {
			summary := strings.TrimSpace(payload.RollingSummary)
			if summary != "" {
				return summary, "ok", nil
			}
		}
	}

	fallback := trimmedPlainText(raw)
	if fallback == "" {
		if err != nil {
			return "", "", err
		}
		return "", "", fmt.Errorf("rolling summary payload empty")
	}
	return fallback, "partial", nil
}

func extractJSONObject(raw string) (string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return "", fmt.Errorf("empty extraction response")
	}

	start := strings.Index(body, "{")
	end := strings.LastIndex(body, "}")
	if start < 0 || end < 0 || end <= start {
		return "", fmt.Errorf("no json object found in response")
	}
	return body[start : end+1], nil
}

func payloadStatus(validCount, dropped int) string {
	switch {
	case validCount == 0 && dropped > 0:
		return "failed"
	case dropped > 0:
		return "partial"
	default:
		return "ok"
	}
}

func trimmedPlainText(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	return raw
}

func splitTopicCandidate(topic string) []string {
	parts := []string{topic}
	separators := []string{"|", ",", ";", " / "}
	for _, separator := range separators {
		next := make([]string, 0, len(parts))
		for _, part := range parts {
			split := strings.Split(part, separator)
			if len(split) == 0 {
				continue
			}
			next = append(next, split...)
		}
		parts = next
	}
	return parts
}
