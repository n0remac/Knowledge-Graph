package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

type Extractor struct {
	client    *ollama.Client
	model     string
	telemetry *telemetry.Manager
}

type topicExtractionPayload struct {
	Topics []string `json:"topics"`
}

type factExtractionPayload struct {
	Facts []factCandidatePayload `json:"facts"`
}

type factCandidatePayload struct {
	Kind       string `json:"kind"`
	AboutRef   string `json:"about_ref"`
	ValueText  string `json:"value_text"`
	Confidence any    `json:"confidence"`
}

type FactCandidate struct {
	Kind       string
	AboutRef   string
	ValueText  string
	Confidence float64
}

const (
	maxFactsPerMessage      = 3
	maxFactValueChars       = 240
	minAcceptedConfidence   = 0.78
	minFallbackConfidence   = 0.88
	maxFallbackFactsPerTurn = 1
)

func NewExtractor(client *ollama.Client, model string, manager *telemetry.Manager) *Extractor {
	return &Extractor{
		client:    client,
		model:     model,
		telemetry: manager,
	}
}

func (e *Extractor) ExtractTopics(ctx context.Context, currentMessage models.Message, extractionCtx models.ExtractionContext) ([]string, error) {
	systemPrompt := `You extract higher-level conversational concepts.
Return ONLY valid JSON with this exact schema:
{
  "topics": ["high-level concept"]
}

Rules:
- Prefer higher-level themes or workstreams over low-level implementation details.
- A good topic should be reusable across multiple future messages.
- Avoid generic labels like "technology", "software", "development", "project".
- Keep each topic short and standalone (2-4 words).
- Return at most 2 topics.
- If uncertain, return fewer topics.`

	userPrompt := buildTopicExtractionPrompt(currentMessage, extractionCtx)
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
	e.emit(ctx, "ollama_request", "topic extraction request prepared", map[string]any{
		"purpose":           "extract_topics",
		"model":             e.model,
		"messages":          messages,
		"system_prompt":     systemPrompt,
		"user_prompt":       userPrompt,
		"request_body_json": string(requestBody),
	})

	result, err := e.client.ChatDetailed(ctx, request)
	if err != nil {
		e.emit(ctx, "ollama_response", "topic extraction request failed", map[string]any{
			"purpose":           "extract_topics",
			"model":             e.model,
			"request_body_json": string(requestBody),
			"raw_response":      result.RawResponse,
			"error":             err.Error(),
		})
		return nil, err
	}
	raw := result.ResponseContent

	payload, err := parseTopicExtractionPayload(raw)
	responsePayload := map[string]any{
		"purpose":      "extract_topics",
		"model":        e.model,
		"raw_response": result.RawResponse,
	}
	if err != nil {
		responsePayload["parse_error"] = err.Error()
		e.emit(ctx, "ollama_response", "topic extraction response parse failed", responsePayload)
		return nil, fmt.Errorf("parse topic extraction payload: %w", err)
	}
	responsePayload["parsed_payload"] = payload
	e.emit(ctx, "ollama_response", "topic extraction response received", responsePayload)

	topics := make([]string, 0, len(payload.Topics))
	for _, topic := range payload.Topics {
		for _, part := range splitTopicCandidate(topic) {
			part = normalizeTopic(part)
			if part == "" {
				continue
			}
			topics = appendIfMissing(topics, part)
		}
	}
	e.emit(ctx, "extract_topics_result", "topic extraction completed", map[string]any{
		"topics": topics,
	})
	return topics, nil
}

func (e *Extractor) ExtractFacts(ctx context.Context, currentMessage models.Message, newTopics, contextTopics []models.Topic, extractionCtx models.ExtractionContext) ([]FactCandidate, error) {
	systemPrompt := `You extract structured conversational facts.
Return ONLY valid JSON with this exact schema:
{
  "facts": [
    {
      "kind": "preference|goal|project|identity|status",
      "about_ref": "user|new_topic:<index>|context_topic:<index>",
      "value_text": "concise factual statement",
      "confidence": 0.0
    }
  ]
}

Rules:
- Extract only stable, reusable memory.
- Prefer linking facts to topics via "new_topic:<index>" or "context_topic:<index>" whenever possible.
- Use "user" only for identity facts or when no topic fits.
- Use "new_topic:<index>" for topics extracted from the current message.
- Use "context_topic:<index>" for concepts established in earlier messages.
- Prefer "context_topic:<index>" when message wording references older concepts like "it/that/this project".
- Keep value_text concise and concrete.
- Split compound statements into multiple atomic facts.
- Return only high-confidence facts that are likely useful in future turns.
- Prefer 1-3 strong facts, not exhaustive extraction.
- If uncertain, return fewer facts.`

	userPrompt := buildFactExtractionPrompt(currentMessage, newTopics, contextTopics, extractionCtx)
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
	e.emit(ctx, "ollama_request", "fact extraction request prepared", map[string]any{
		"purpose":           "extract_facts",
		"model":             e.model,
		"messages":          messages,
		"system_prompt":     systemPrompt,
		"user_prompt":       userPrompt,
		"request_body_json": string(requestBody),
	})

	result, err := e.client.ChatDetailed(ctx, request)
	if err != nil {
		e.emit(ctx, "ollama_response", "fact extraction request failed", map[string]any{
			"purpose":           "extract_facts",
			"model":             e.model,
			"request_body_json": string(requestBody),
			"raw_response":      result.RawResponse,
			"error":             err.Error(),
		})
		return nil, err
	}
	raw := result.ResponseContent

	payload, err := parseFactExtractionPayload(raw)
	responsePayload := map[string]any{
		"purpose":      "extract_facts",
		"model":        e.model,
		"raw_response": result.RawResponse,
	}
	if err != nil {
		responsePayload["parse_error"] = err.Error()
		e.emit(ctx, "ollama_response", "fact extraction response parse failed", responsePayload)
		return nil, fmt.Errorf("parse fact extraction payload: %w", err)
	}
	responsePayload["parsed_payload"] = payload
	e.emit(ctx, "ollama_response", "fact extraction response received", responsePayload)

	preferredTopicRef := preferredTopicRef(newTopics, contextTopics)
	facts := make([]FactCandidate, 0, len(payload.Facts))
	seen := make(map[string]struct{}, len(payload.Facts))
	for _, fact := range payload.Facts {
		kind := normalizeFactKind(fact.Kind)
		aboutRef := normalizeAboutRef(fact.AboutRef)
		if aboutRef == "" {
			aboutRef = fallbackAboutRefForKind(kind, preferredTopicRef)
		}
		if shouldRouteFactToTopic(kind, aboutRef, preferredTopicRef) {
			aboutRef = preferredTopicRef
		}
		value := normalizeFactValue(fact.ValueText)
		confidence := normalizeConfidence(fact.Confidence)
		if kind == "" || aboutRef == "" || value == "" || confidence < minAcceptedConfidence {
			continue
		}
		if isMessageEchoFact(value, currentMessage.Content) {
			continue
		}

		factKey := fmt.Sprintf("%s|%s|%s", kind, aboutRef, canonicalText(value))
		if _, exists := seen[factKey]; exists {
			continue
		}
		seen[factKey] = struct{}{}

		facts = append(facts, FactCandidate{
			Kind:       kind,
			AboutRef:   aboutRef,
			ValueText:  value,
			Confidence: confidence,
		})
		if len(facts) >= maxFactsPerMessage {
			break
		}
	}

	if len(facts) == 0 {
		for _, fallback := range buildFallbackFacts(currentMessage.Content, preferredTopicRef, maxFallbackFactsPerTurn) {
			factKey := fmt.Sprintf("%s|%s|%s", fallback.Kind, fallback.AboutRef, canonicalText(fallback.ValueText))
			if _, exists := seen[factKey]; exists {
				continue
			}
			seen[factKey] = struct{}{}
			facts = append(facts, fallback)
			if len(facts) >= maxFactsPerMessage {
				break
			}
		}
	}
	e.emit(ctx, "extract_facts_result", "fact extraction completed", map[string]any{
		"facts": factCandidatesToPayload(facts),
	})
	return facts, nil
}

func (e *Extractor) emit(ctx context.Context, kind, summary string, payload map[string]any) {
	if e == nil || e.telemetry == nil {
		return
	}
	e.telemetry.Emit(ctx, telemetry.StageExtraction, kind, summary, payload)
}

func factCandidatesToPayload(facts []FactCandidate) []map[string]any {
	out := make([]map[string]any, 0, len(facts))
	for _, fact := range facts {
		out = append(out, map[string]any{
			"kind":       fact.Kind,
			"about_ref":  fact.AboutRef,
			"value_text": fact.ValueText,
			"confidence": fact.Confidence,
		})
	}
	return out
}

func buildTopicExtractionPrompt(currentMessage models.Message, extractionCtx models.ExtractionContext) string {
	var builder strings.Builder
	builder.WriteString("current_message:\n")
	builder.WriteString(currentMessage.Content)
	builder.WriteString("\n\nrecent_messages:\n")
	builder.WriteString(formatMessagesForPrompt(extractionCtx.RecentMessages, currentMessage.ID, 8))

	builder.WriteString("\nreply_target:\n")
	if extractionCtx.ReplyMessage == nil || strings.TrimSpace(extractionCtx.ReplyMessage.Content) == "" {
		builder.WriteString("none")
	} else {
		builder.WriteString(extractionCtx.ReplyMessage.Content)
	}
	return builder.String()
}

func buildFactExtractionPrompt(currentMessage models.Message, newTopics, contextTopics []models.Topic, extractionCtx models.ExtractionContext) string {
	var builder strings.Builder
	builder.WriteString("current_message:\n")
	builder.WriteString(currentMessage.Content)
	builder.WriteString("\n\nrecent_messages:\n")
	builder.WriteString(formatMessagesForPrompt(extractionCtx.RecentMessages, currentMessage.ID, 8))

	builder.WriteString("\nrecent_durable_facts_for_speaker:\n")
	if len(extractionCtx.RecentDurableFacts) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, fact := range extractionCtx.RecentDurableFacts {
			builder.WriteString("- kind=")
			builder.WriteString(fact.Kind)
			builder.WriteString(" about=")
			builder.WriteString(fact.AboutType)
			builder.WriteString(":")
			builder.WriteString(fact.AboutID)
			builder.WriteString(" value=")
			builder.WriteString(fact.ValueText)
			builder.WriteString("\n")
		}
	}

	builder.WriteString("\nnew_topics:\n")
	if len(newTopics) == 0 {
		builder.WriteString("- none\n")
	} else {
		for i, topic := range newTopics {
			builder.WriteString(fmt.Sprintf("- [%d] %s\n", i, topic.Name))
		}
	}

	builder.WriteString("\ncontext_topics:\n")
	if len(contextTopics) == 0 {
		builder.WriteString("- none\n")
	} else {
		for i, topic := range contextTopics {
			builder.WriteString(fmt.Sprintf("- [%d] %s\n", i, topic.Name))
		}
	}

	builder.WriteString("\nreply_target:\n")
	if extractionCtx.ReplyMessage == nil || strings.TrimSpace(extractionCtx.ReplyMessage.Content) == "" {
		builder.WriteString("none")
	} else {
		builder.WriteString(extractionCtx.ReplyMessage.Content)
	}
	return builder.String()
}

func formatMessagesForPrompt(messages []models.Message, skipMessageID string, limit int) string {
	if limit <= 0 {
		limit = 1
	}

	start := 0
	if len(messages) > limit {
		start = len(messages) - limit
	}

	var builder strings.Builder
	for _, message := range messages[start:] {
		if message.ID == skipMessageID {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		builder.WriteString("- ")
		builder.WriteString(message.AuthorID)
		builder.WriteString(": ")
		builder.WriteString(content)
		builder.WriteString("\n")
	}

	if builder.Len() == 0 {
		return "- none\n"
	}
	return builder.String()
}

func parseTopicExtractionPayload(raw string) (topicExtractionPayload, error) {
	body, err := extractJSONObject(raw)
	if err != nil {
		return topicExtractionPayload{}, err
	}

	var payload topicExtractionPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return topicExtractionPayload{}, err
	}
	return payload, nil
}

func parseFactExtractionPayload(raw string) (factExtractionPayload, error) {
	body, err := extractJSONObject(raw)
	if err != nil {
		return factExtractionPayload{}, err
	}

	var payload factExtractionPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return factExtractionPayload{}, err
	}
	return payload, nil
}

func extractJSONObject(raw string) (string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return "", fmt.Errorf("empty extraction response")
	}

	start := strings.Index(body, "{")
	end := strings.LastIndex(body, "}")
	if start < 0 || end < 0 || end <= start {
		return "", fmt.Errorf("no JSON object found in response")
	}
	return body[start : end+1], nil
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

func normalizeTopic(topic string) string {
	topic = strings.ToLower(strings.TrimSpace(topic))
	topic = strings.Join(strings.Fields(topic), " ")
	topic = strings.TrimPrefix(topic, "the ")
	return topic
}

func appendIfMissing(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func normalizeFactKind(raw string) string {
	kind := strings.ToLower(strings.TrimSpace(raw))
	if kind == "" {
		return ""
	}

	parts := splitFactKind(kind)
	for _, part := range parts {
		if normalized, ok := canonicalFactKind(part); ok {
			return normalized
		}
	}
	return ""
}

func splitFactKind(kind string) []string {
	fields := strings.FieldsFunc(kind, func(r rune) bool {
		switch r {
		case '|', ',', ';', '/', '\\':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		out = append(out, field)
	}
	if len(out) == 0 && kind != "" {
		return []string{kind}
	}
	return out
}

func canonicalFactKind(kind string) (string, bool) {
	switch kind {
	case "preference", "constraint", "requirement":
		return "preference", true
	case "goal", "objective", "intent", "plan":
		return "goal", true
	case "project", "work", "initiative":
		return "project", true
	case "identity", "role", "about_me":
		return "identity", true
	case "status", "state", "progress":
		return "status", true
	default:
		return "", false
	}
}

func normalizeAboutRef(raw string) string {
	ref := strings.ToLower(strings.TrimSpace(raw))
	if ref == "" {
		return ""
	}
	ref = strings.ReplaceAll(ref, " ", "")
	ref = strings.ReplaceAll(ref, "-", "_")

	switch ref {
	case "user", "self", "speaker", "author":
		return "user"
	}

	if strings.HasPrefix(ref, "new_topic:") {
		if normalized, ok := normalizeTopicRef("new_topic", strings.TrimPrefix(ref, "new_topic:")); ok {
			return normalized
		}
	}
	if strings.HasPrefix(ref, "newtopic:") {
		if normalized, ok := normalizeTopicRef("new_topic", strings.TrimPrefix(ref, "newtopic:")); ok {
			return normalized
		}
	}
	if strings.HasPrefix(ref, "context_topic:") {
		if normalized, ok := normalizeTopicRef("context_topic", strings.TrimPrefix(ref, "context_topic:")); ok {
			return normalized
		}
	}
	if strings.HasPrefix(ref, "contexttopic:") {
		if normalized, ok := normalizeTopicRef("context_topic", strings.TrimPrefix(ref, "contexttopic:")); ok {
			return normalized
		}
	}
	return ""
}

func normalizeTopicRef(prefix, rawIndex string) (string, bool) {
	index, err := strconv.Atoi(strings.TrimSpace(rawIndex))
	if err != nil || index < 0 {
		return "", false
	}
	return fmt.Sprintf("%s:%d", prefix, index), true
}

func preferredTopicRef(newTopics, contextTopics []models.Topic) string {
	if len(newTopics) > 0 {
		return "new_topic:0"
	}
	if len(contextTopics) > 0 {
		return "context_topic:0"
	}
	return ""
}

func fallbackAboutRefForKind(kind, preferred string) string {
	if kind == "identity" || preferred == "" {
		return "user"
	}
	return preferred
}

func shouldRouteFactToTopic(kind, aboutRef, preferred string) bool {
	if preferred == "" {
		return false
	}
	if aboutRef != "user" {
		return false
	}
	return kind != "identity"
}

func normalizeFactValue(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "\"'")
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) > maxFactValueChars {
		value = strings.TrimSpace(string(runes[:maxFactValueChars]))
	}
	return value
}

func canonicalText(raw string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
}

func isMessageEchoFact(value, message string) bool {
	valueCanonical := canonicalText(value)
	messageCanonical := canonicalText(message)
	if valueCanonical == "" || messageCanonical == "" {
		return false
	}
	if valueCanonical == messageCanonical {
		return true
	}
	if len(valueCanonical) > 160 && strings.Contains(messageCanonical, valueCanonical) {
		return true
	}
	return false
}

func normalizeConfidence(raw any) float64 {
	confidence := 0.5
	switch v := raw.(type) {
	case nil:
		return confidence
	case float64:
		confidence = v
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			confidence = parsed
		}
	}
	if confidence < 0 {
		return 0
	}
	if confidence > 1 {
		return 1
	}
	return confidence
}

func buildFallbackFacts(messageContent, preferredTopicRef string, needed int) []FactCandidate {
	if needed <= 0 {
		return nil
	}

	aboutRef := "user"
	if preferredTopicRef != "" {
		aboutRef = preferredTopicRef
	}

	added := make(map[string]struct{}, 4)
	out := make([]FactCandidate, 0, needed)
	add := func(kind, value string, confidence float64) {
		if len(out) >= needed {
			return
		}
		kind = normalizeFactKind(kind)
		value = normalizeFactValue(value)
		confidence = normalizeConfidence(confidence)
		if kind == "" || value == "" || confidence < minFallbackConfidence {
			return
		}
		key := fmt.Sprintf("%s|%s|%s", kind, aboutRef, canonicalText(value))
		if _, exists := added[key]; exists {
			return
		}
		added[key] = struct{}{}
		out = append(out, FactCandidate{
			Kind:       kind,
			AboutRef:   fallbackAboutRefForKind(kind, preferredTopicRef),
			ValueText:  value,
			Confidence: confidence,
		})
	}

	lower := strings.ToLower(messageContent)
	if clause := extractClauseAfter(messageContent, "i am "); clause != "" && containsAny(strings.ToLower(clause), "building", "making", "working on", "developing", "creating") {
		add("project", "Current work: "+clause, 0.9)
	}
	if clause := extractClauseAfter(messageContent, "i want "); clause != "" {
		add("goal", "Goal: "+clause, 0.91)
	}
	if clause := extractClauseAfter(messageContent, "the goal is to "); clause != "" {
		add("goal", "Goal: "+clause, 0.92)
	}
	if strings.Contains(lower, "run locally") || strings.Contains(lower, "running locally") || strings.Contains(lower, "local-only") {
		add("preference", "Prefers local-only runtime.", 0.9)
	}
	if strings.Contains(lower, "basics working") || strings.Contains(lower, "basic setup working") {
		add("status", "Has the basic setup working.", 0.9)
	}

	return out
}

func extractClauseAfter(content, marker string) string {
	lower := strings.ToLower(content)
	start := strings.Index(lower, marker)
	if start < 0 {
		return ""
	}
	clause := strings.TrimSpace(content[start+len(marker):])
	if clause == "" {
		return ""
	}
	end := len(clause)
	for i, ch := range clause {
		if ch == '.' || ch == '!' || ch == '?' || ch == ';' || ch == '\n' {
			end = i
			break
		}
	}
	clause = strings.TrimSpace(clause[:end])
	clause = strings.Trim(clause, ", ")
	return strings.Join(strings.Fields(clause), " ")
}

func containsAny(content string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(content, needle) {
			return true
		}
	}
	return false
}
