package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Message ChatMessage `json:"message"`
}

type ChatResult struct {
	Request         ChatRequest `json:"request"`
	RequestBodyJSON string      `json:"request_body_json"`
	RawResponse     string      `json:"raw_response"`
	ResponseContent string      `json:"response_content"`
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Chat(ctx context.Context, model string, messages []ChatMessage) (string, error) {
	result, err := c.ChatDetailed(ctx, ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	})
	if err != nil {
		return "", err
	}
	return result.ResponseContent, nil
}

func (c *Client) ChatDetailed(ctx context.Context, payload ChatRequest) (ChatResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResult{}, fmt.Errorf("marshal chat payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, fmt.Errorf("create chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	result := ChatResult{
		Request:         payload,
		RequestBodyJSON: string(body),
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return result, fmt.Errorf("ollama request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, fmt.Errorf("read chat response: %w", err)
	}
	result.RawResponse = string(respBody)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("ollama status %d: %s", resp.StatusCode, strings.TrimSpace(result.RawResponse))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return result, fmt.Errorf("decode chat response: %w", err)
	}

	result.ResponseContent = parsed.Message.Content
	return result, nil
}
