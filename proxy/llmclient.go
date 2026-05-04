package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LLMClient is a simple Anthropic-format HTTP client that calls the local proxy
type LLMClient struct {
	baseURL    string
	authToken  string
	model      string
	httpClient *http.Client
}

// NewLLMClient creates a client that talks to the proxy at baseURL
func NewLLMClient(baseURL, authToken, model string) *LLMClient {
	return &LLMClient{
		baseURL:    baseURL,
		authToken:  authToken,
		model:      model,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Chat sends a user message and returns the assistant text response
func (c *LLMClient) Chat(userMessage string) (string, error) {
	return c.ChatCtx(context.Background(), userMessage)
}

// ChatCtx sends a user message using the provided context, enabling cancellation.
func (c *LLMClient) ChatCtx(ctx context.Context, userMessage string) (string, error) {
	body := map[string]interface{}{
		"model":      c.model,
		"max_tokens": 4096,
		"messages": []map[string]interface{}{
			{"role": "user", "content": userMessage},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if c.authToken != "" {
		req.Header.Set("x-api-key", c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("proxy returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	for _, block := range result.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in response")
}
