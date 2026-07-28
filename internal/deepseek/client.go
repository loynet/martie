package deepseek

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

const (
	defaultBaseURL   = "https://api.deepseek.com"
	maxResponseBytes = 1 << 20
)

type Client struct {
	APIKey     string
	Model      string
	MaxTokens  int
	BaseURL    string
	HTTPClient *http.Client
}

type Completion struct {
	Text         string
	FinishReason FinishReason
	Usage        Usage
}

type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishContentFilter FinishReason = "content_filter"
)

type Usage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
}

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("deepseek api error: status %d: %s", e.StatusCode, e.Message)
}

func New(apiKey, model string, maxTokens int, timeout time.Duration) *Client {
	return &Client{
		APIKey:     apiKey,
		Model:      model,
		MaxTokens:  maxTokens,
		BaseURL:    defaultBaseURL,
		HTTPClient: &http.Client{Timeout: timeout},
	}
}

func (c *Client) Complete(ctx context.Context, systemPrompt string, conversation []Message) (Completion, error) {
	messages := make([]Message, 0, len(conversation)+1)
	if systemPrompt != "" {
		messages = append(messages, Message{Role: RoleSystem, Content: systemPrompt})
	}
	messages = append(messages, conversation...)

	body, err := json.Marshal(completionRequest{
		Model:     c.Model,
		Messages:  messages,
		Thinking:  thinking{Type: "disabled"},
		MaxTokens: c.MaxTokens,
	})
	if err != nil {
		return Completion{}, fmt.Errorf("encode completion request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("create completion request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Completion{}, fmt.Errorf("send completion request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result errorResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&result); err != nil || result.Error.Message == "" {
			result.Error.Message = resp.Status
		}
		return Completion{}, &APIError{StatusCode: resp.StatusCode, Message: result.Error.Message}
	}

	var result completionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&result); err != nil {
		return Completion{}, fmt.Errorf("decode completion response: %w", err)
	}
	if len(result.Choices) == 0 {
		return Completion{}, fmt.Errorf("completion response has no choices")
	}
	choice := result.Choices[0]
	text := strings.TrimSpace(choice.Message.Content)
	if text == "" && choice.FinishReason == FinishStop {
		return Completion{}, fmt.Errorf("completion response is empty")
	}

	return Completion{
		Text:         text,
		FinishReason: choice.FinishReason,
		Usage:        result.Usage,
	}, nil
}

type thinking struct {
	Type string `json:"type"`
}

type completionRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	Thinking  thinking  `json:"thinking"`
	MaxTokens int       `json:"max_tokens"`
}

type completionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason FinishReason `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

type errorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}
