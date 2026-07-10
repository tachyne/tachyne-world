package server

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

// llmClient talks to an OpenAI-compatible chat endpoint — e.g. LM Studio's local
// server (http://host:1234/v1). Pure stdlib (net/http + json): no SDK, no API key,
// no new dependency. Used by the LLM-driven NPC brain (see npc.go).
type llmClient struct {
	baseURL string // e.g. http://localhost:1234/v1
	model   string
	http    *http.Client
}

func newLLMClient(baseURL, model string) *llmClient {
	return &llmClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

// responseFormat asks for a JSON object back (LM Studio honours this for models
// that support structured output; harmless otherwise — we also defensively
// extract the JSON from the reply).
type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// complete sends a system+user chat and returns the assistant's reply text.
func (c *llmClient) complete(ctx context.Context, system, user string) (string, error) {
	// Gemma (and some other local models) have no system role and reject
	// response_format=json_object, so we fold the instructions into the user turn
	// and rely on the prompt + extractJSON instead of forced JSON mode.
	reqBody, _ := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "user", Content: system + "\n\n" + user},
		},
		Temperature: 0.8,
		MaxTokens:   200,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("llm %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", err
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm: empty response")
	}
	return cr.Choices[0].Message.Content, nil
}

// extractJSON pulls the first {...} object out of a model reply, tolerating any
// stray prose a local model wraps around it.
func extractJSON(s string) string {
	i := strings.IndexByte(s, '{')
	j := strings.LastIndexByte(s, '}')
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}
