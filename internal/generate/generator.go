package generate

import (
	"context"
	"fmt"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/models"
	"github.com/n0remac/Knowledge-Graph/internal/ollama"
)

type Generator struct {
	client  *ollama.Client
	model   string
	persona string
}

func NewGenerator(client *ollama.Client, model, persona string) *Generator {
	return &Generator{
		client:  client,
		model:   model,
		persona: persona,
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
	reply, err := g.client.Chat(ctx, g.model, []ollama.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	})
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(reply), nil
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

	builder.WriteString("\nRelevant memory facts:\n")
	if len(bundle.Facts) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, fact := range bundle.Facts {
			builder.WriteString("- [")
			builder.WriteString(fact.Status)
			builder.WriteString(" conf=")
			builder.WriteString(fmt.Sprintf("%.2f", fact.Confidence))
			builder.WriteString("] user=")
			builder.WriteString(fact.SubjectID)
			builder.WriteString(" kind=")
			builder.WriteString(fact.Kind)
			if fact.ObjectID != "" {
				builder.WriteString(" object=")
				builder.WriteString(fact.ObjectID)
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
