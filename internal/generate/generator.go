package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
	"github.com/n0remac/Knowledge-Graph/internal/telemetry"
)

type Generator struct {
	client    *ollama.Client
	model     string
	persona   string
	telemetry *telemetry.Manager
}

func NewGenerator(client *ollama.Client, model, persona string, manager *telemetry.Manager) *Generator {
	return &Generator{
		client:    client,
		model:     model,
		persona:   persona,
		telemetry: manager,
	}
}

func (g *Generator) GenerateReply(ctx context.Context, currentMessage models.Message, bundle models.RetrievalBundle) (string, error) {
	systemPrompt := fmt.Sprintf(
		`You are a Discord chatbot.
Persona: %s

Instructions:
- Use the provided memory context when relevant.
- Prefer durable memories when confidence is low.
- Do not invent facts not in memory or the current message.
- If uncertain, phrase cautiously.
- Keep replies concise and natural for Discord.`,
		g.persona,
	)

	userPrompt := buildUserPrompt(currentMessage, bundle)
	messages := []ollama.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	request := ollama.ChatRequest{
		Model:    g.model,
		Messages: messages,
		Stream:   false,
	}
	requestBody, _ := json.Marshal(request)
	g.emit(ctx, "ollama_request", "reply generation request prepared", map[string]any{
		"purpose":           "generate_reply",
		"model":             g.model,
		"messages":          messages,
		"system_prompt":     systemPrompt,
		"user_prompt":       userPrompt,
		"request_body_json": string(requestBody),
	})

	result, err := g.client.ChatDetailed(ctx, request)
	if err != nil {
		g.emit(ctx, "ollama_response", "reply generation request failed", map[string]any{
			"purpose":           "generate_reply",
			"model":             g.model,
			"request_body_json": string(requestBody),
			"raw_response":      result.RawResponse,
			"error":             err.Error(),
		})
		return "", err
	}

	g.emit(ctx, "ollama_response", "reply generation response received", map[string]any{
		"purpose":      "generate_reply",
		"model":        g.model,
		"raw_response": result.RawResponse,
	})

	reply := strings.TrimSpace(result.ResponseContent)
	g.emit(ctx, "generate_reply_result", "reply generation completed", map[string]any{
		"reply":        reply,
		"reply_length": len([]rune(reply)),
		"model":        g.model,
	})
	return reply, nil
}

func (g *Generator) emit(ctx context.Context, kind, summary string, payload map[string]any) {
	if g == nil || g.telemetry == nil {
		return
	}
	g.telemetry.Emit(ctx, telemetry.StageGeneration, kind, summary, payload)
}

func buildUserPrompt(currentMessage models.Message, bundle models.RetrievalBundle) string {
	var builder strings.Builder

	builder.WriteString("Recent conversation:\n")
	for _, message := range bundle.RecentMessages {
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		author := message.AuthorID
		if author == "" {
			author = "unknown_user"
		}
		builder.WriteString("- ")
		builder.WriteString(author)
		builder.WriteString(": ")
		builder.WriteString(message.Content)
		builder.WriteString("\n")
	}

	builder.WriteString("\nUser memory facts:\n")
	if len(bundle.UserFacts) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, fact := range bundle.UserFacts {
			builder.WriteString("- [")
			builder.WriteString(fact.Status)
			builder.WriteString(" conf=")
			builder.WriteString(fmt.Sprintf("%.2f", fact.Confidence))
			builder.WriteString("] user=")
			builder.WriteString(fact.DiscordUserID)
			builder.WriteString(" kind=")
			builder.WriteString(fact.Kind)
			builder.WriteString(" about=user:")
			builder.WriteString(fact.AboutID)
			builder.WriteString(" value=")
			builder.WriteString(fact.ValueText)
			builder.WriteString("\n")
		}
	}

	topicNameByID := make(map[string]string, len(bundle.Topics))
	for _, topic := range bundle.Topics {
		topicNameByID[fmt.Sprintf("%d", topic.ID)] = topic.Name
	}

	builder.WriteString("\nConcept memory facts:\n")
	if len(bundle.TopicFacts) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, fact := range bundle.TopicFacts {
			builder.WriteString("- [")
			builder.WriteString(fact.Status)
			builder.WriteString(" conf=")
			builder.WriteString(fmt.Sprintf("%.2f", fact.Confidence))
			builder.WriteString("] kind=")
			builder.WriteString(fact.Kind)
			builder.WriteString(" about=topic:")
			if topicName, ok := topicNameByID[fact.AboutID]; ok {
				builder.WriteString(topicName)
			} else {
				builder.WriteString(fact.AboutID)
			}
			builder.WriteString(" value=")
			builder.WriteString(fact.ValueText)
			builder.WriteString("\n")
		}
	}

	builder.WriteString("\nRelevant topics:\n")
	if len(bundle.Topics) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, topic := range bundle.Topics {
			builder.WriteString("- ")
			builder.WriteString(topic.Name)
			builder.WriteString("\n")
		}
	}

	builder.WriteString("\nCurrent user message:\n")
	builder.WriteString(currentMessage.Content)

	return builder.String()
}
