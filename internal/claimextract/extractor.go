package claimextract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

const (
	telemetryKindRequest  = "ollama_request"
	telemetryKindResponse = "ollama_response"
)

type Input struct {
	CurrentMessage models.RawMessage
	ReplyTarget    *models.RawMessage
	RecentMessages []models.RawMessage
	WorkingState   models.WorkingState
}

type Result struct {
	Claims []models.Claim
	Status string
	Raw    string
	Model  string
	Err    error
}

type Extractor struct {
	client    *ollama.Client
	model     string
	stage     string
	telemetry *telemetry.Manager
}

func New(client *ollama.Client, model, stage string, manager *telemetry.Manager) *Extractor {
	return &Extractor{
		client:    client,
		model:     strings.TrimSpace(model),
		stage:     strings.TrimSpace(stage),
		telemetry: manager,
	}
}

func (e *Extractor) Extract(ctx context.Context, input Input) Result {
	if e == nil || e.client == nil {
		err := fmt.Errorf("claims extractor is not initialized")
		return Result{Status: "failed", Model: strings.TrimSpace(e.model), Err: err}
	}

	systemPrompt := `You extract lightweight conversational claims.
Return ONLY valid JSON with this exact schema:
{
  "claims": [
    {
      "subject": "who or what",
      "predicate": "relationship or action",
      "object": "target or value"
    }
  ]
}

Rules:
- Extract at most 4 claims.
- Keep claims concise and literal.
- Use only information grounded in the current message plus the provided short context.
- Prefer claims that help maintain short-term continuity.
- If there are no meaningful claims, return {"claims":[]}.`

	userPrompt := buildPromptEnvelope(input)
	raw, model, err := e.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		return Result{Status: "failed", Raw: raw, Model: model, Err: err}
	}

	claims, status, parseErr := parseClaimsPayload(raw, input.CurrentMessage.MessageID)
	if parseErr != nil {
		return Result{Status: "failed", Raw: raw, Model: model, Err: parseErr}
	}
	return Result{Claims: claims, Status: status, Raw: raw, Model: model}
}

func (e *Extractor) callLLM(ctx context.Context, systemPrompt, userPrompt string) (string, string, error) {
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
	e.emit(ctx, telemetryKindRequest, "extract_claims request prepared", map[string]any{
		"purpose":           "extract_claims",
		"model":             e.model,
		"messages":          messages,
		"system_prompt":     systemPrompt,
		"user_prompt":       userPrompt,
		"request_body_json": string(requestBody),
	})

	result, err := e.client.ChatDetailed(ctx, request)
	if err != nil {
		e.emit(ctx, telemetryKindResponse, "extract_claims request failed", map[string]any{
			"purpose":           "extract_claims",
			"model":             e.model,
			"request_body_json": string(requestBody),
			"raw_response":      result.RawResponse,
			"error":             err.Error(),
		})
		return strings.TrimSpace(result.ResponseContent), e.model, err
	}

	e.emit(ctx, telemetryKindResponse, "extract_claims response received", map[string]any{
		"purpose":      "extract_claims",
		"model":        e.model,
		"raw_response": result.RawResponse,
	})
	return strings.TrimSpace(result.ResponseContent), e.model, nil
}

func buildPromptEnvelope(input Input) string {
	var builder strings.Builder
	builder.WriteString("current_message:\n")
	builder.WriteString(strings.TrimSpace(input.CurrentMessage.Content))
	builder.WriteString("\n\nauthor_role:\n")
	builder.WriteString(strings.TrimSpace(input.CurrentMessage.AuthorRole))

	builder.WriteString("\n\nreply_target:\n")
	if input.ReplyTarget == nil || strings.TrimSpace(input.ReplyTarget.Content) == "" {
		builder.WriteString("none")
	} else {
		builder.WriteString(strings.TrimSpace(input.ReplyTarget.Content))
	}

	builder.WriteString("\n\nrecent_messages:\n")
	builder.WriteString(formatRawMessages(input.RecentMessages))

	builder.WriteString("\ncurrent_rolling_summary:\n")
	if strings.TrimSpace(input.WorkingState.RollingSummary) == "" {
		builder.WriteString("none")
	} else {
		builder.WriteString(strings.TrimSpace(input.WorkingState.RollingSummary))
	}

	builder.WriteString("\n\ncurrent_active_topics:\n")
	if len(input.WorkingState.ActiveTopics) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, topic := range input.WorkingState.ActiveTopics {
			builder.WriteString("- ")
			builder.WriteString(strings.TrimSpace(topic.Name))
			builder.WriteString(" (status=")
			builder.WriteString(strings.TrimSpace(topic.Status))
			builder.WriteString(")\n")
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
		builder.WriteString(strings.TrimSpace(message.AuthorRole))
		builder.WriteString(":")
		builder.WriteString(strings.TrimSpace(message.AuthorID))
		builder.WriteString(": ")
		builder.WriteString(strings.TrimSpace(message.Content))
		builder.WriteString("\n")
	}
	if builder.Len() == 0 {
		return "- none\n"
	}
	return builder.String()
}

type claimsPayload struct {
	Claims []claimPayload `json:"claims"`
}

type claimPayload struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
}

func parseClaimsPayload(raw, sourceMessageID string) ([]models.Claim, string, error) {
	body, err := extractJSONObject(raw)
	if err != nil {
		return nil, "", err
	}
	var payload claimsPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, "", err
	}

	out := make([]models.Claim, 0, len(payload.Claims))
	dropped := 0
	for _, claim := range payload.Claims {
		subject := strings.TrimSpace(claim.Subject)
		predicate := strings.TrimSpace(claim.Predicate)
		object := strings.TrimSpace(claim.Object)
		if subject == "" || predicate == "" || object == "" {
			dropped++
			continue
		}
		out = append(out, models.Claim{
			Subject:         subject,
			Predicate:       predicate,
			Object:          object,
			SourceMessageID: sourceMessageID,
		})
	}
	return out, payloadStatus(len(out), dropped), nil
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

func (e *Extractor) emit(ctx context.Context, kind, summary string, payload map[string]any) {
	if e == nil || e.telemetry == nil {
		return
	}
	stage := e.stage
	if stage == "" {
		stage = telemetry.StageConversation
	}
	e.telemetry.Emit(ctx, stage, kind, summary, payload)
}
