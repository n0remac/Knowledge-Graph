package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
)

type Extractor struct {
	client *ollama.Client
	model  string
}

type ExtractionResult struct {
	Topics []string
	Facts  []models.FactInput
}

type extractionPayload struct {
	Topics []string           `json:"topics"`
	Facts  []factPayloadEntry `json:"facts"`
}

type factPayloadEntry struct {
	Kind       string   `json:"kind"`
	SubjectID  string   `json:"subject_id"`
	ObjectID   string   `json:"object_id"`
	ValueText  string   `json:"value_text"`
	Confidence *float64 `json:"confidence"`
}

func NewExtractor(client *ollama.Client, model string) *Extractor {
	return &Extractor{
		client: client,
		model:  model,
	}
}

func (e *Extractor) ExtractFromMessage(ctx context.Context, message models.Message) (ExtractionResult, error) {
	systemPrompt := `You extract structured conversational memory.
Return ONLY valid JSON with this exact schema:
{
  "topics": ["short normalized topic"],
  "facts": [
    {
      "kind": "preference|goal|project|relationship|identity|status",
      "subject_id": "discord user id",
      "object_id": "discord user id or empty string",
      "value_text": "concise factual statement",
      "confidence": 0.0
    }
  ]
}

Rules:
- Extract only stable or reusable information.
- Prefer explicit statements from the user.
- Do not infer private details.
- Keep topics short and lowercase-friendly.
- If unsure, return fewer items.`

	userPrompt := fmt.Sprintf(
		"message_author_id: %s\nmessage_author_name: %s\nmessage_text: %s",
		message.AuthorID,
		message.Author,
		message.Content,
	)

	raw, err := e.client.Chat(ctx, e.model, []ollama.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	})
	if err != nil {
		return ExtractionResult{}, err
	}

	payload, err := parseExtractionPayload(raw)
	if err != nil {
		return ExtractionResult{}, fmt.Errorf("parse extraction payload: %w", err)
	}

	result := ExtractionResult{
		Topics: make([]string, 0, len(payload.Topics)),
		Facts:  make([]models.FactInput, 0, len(payload.Facts)),
	}

	for _, topic := range payload.Topics {
		topic = normalizeTopic(topic)
		if topic == "" {
			continue
		}
		result.Topics = appendIfMissing(result.Topics, topic)
	}

	for _, fact := range payload.Facts {
		kind := strings.ToLower(strings.TrimSpace(fact.Kind))
		subject := strings.TrimSpace(fact.SubjectID)
		value := strings.TrimSpace(fact.ValueText)
		if subject == "" {
			subject = message.AuthorID
		}
		if kind == "" || value == "" {
			continue
		}

		confidence := 0.5
		if fact.Confidence != nil {
			confidence = *fact.Confidence
		}
		if confidence < 0 {
			confidence = 0
		}
		if confidence > 1 {
			confidence = 1
		}

		result.Facts = append(result.Facts, models.FactInput{
			Kind:       kind,
			SubjectID:  subject,
			ObjectID:   strings.TrimSpace(fact.ObjectID),
			ValueText:  value,
			Confidence: confidence,
		})
	}

	return result, nil
}

func parseExtractionPayload(raw string) (extractionPayload, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return extractionPayload{}, fmt.Errorf("empty extraction response")
	}

	start := strings.Index(body, "{")
	end := strings.LastIndex(body, "}")
	if start < 0 || end < 0 || end <= start {
		return extractionPayload{}, fmt.Errorf("no JSON object found in response")
	}
	body = body[start : end+1]

	var payload extractionPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return extractionPayload{}, err
	}
	return payload, nil
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
