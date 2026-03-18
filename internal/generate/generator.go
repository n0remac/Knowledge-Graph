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

func (g *Generator) GenerateReplyFromBrief(ctx context.Context, currentMessage models.RawMessage, contextBrief string) (string, error) {
	systemPrompt := fmt.Sprintf(
		`You are a Discord chatbot.
Persona: %s

Instructions:
- Use the provided conversation brief when relevant.
- Do not invent facts outside the brief or latest user message.
- If uncertain, phrase cautiously.
- Keep replies concise and natural for Discord.`,
		g.persona,
	)

	userPrompt := buildBriefPrompt(currentMessage, contextBrief)
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

func buildBriefPrompt(currentMessage models.RawMessage, contextBrief string) string {
	var builder strings.Builder
	builder.WriteString("Conversation brief:\n")
	if strings.TrimSpace(contextBrief) == "" {
		builder.WriteString("No conversation brief available.\n")
	} else {
		builder.WriteString(contextBrief)
		builder.WriteString("\n")
	}
	builder.WriteString("\nLatest user message:\n")
	builder.WriteString(currentMessage.Content)
	return builder.String()
}
