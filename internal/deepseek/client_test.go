package deepseek

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClientComplete(t *testing.T) {
	var gotRequest *http.Request
	client := New("secret", "deepseek-v4-flash", 500, 0)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotRequest = req
		return response(http.StatusOK, `{
			"choices":[{"message":{"content":" Hello there. "},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_cache_hit_tokens":4,"prompt_cache_miss_tokens":8}
		}`), nil
	})}

	completion, err := client.Complete(context.Background(), "Be concise.", []Message{{Role: RoleUser, Content: `hello "bot"`}})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completion.Text != "Hello there." || completion.FinishReason != FinishStop {
		t.Fatalf("Complete() = %+v", completion)
	}
	if completion.Usage.PromptTokens != 12 || completion.Usage.CompletionTokens != 3 {
		t.Fatalf("Complete() usage = %+v", completion.Usage)
	}

	if got := gotRequest.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("Authorization = %q", got)
	}
	var body completionRequest
	if err := json.NewDecoder(gotRequest.Body).Decode(&body); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if body.Model != "deepseek-v4-flash" || body.MaxTokens != 500 || body.Thinking.Type != "disabled" {
		t.Fatalf("request = %+v", body)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[0].Content != "Be concise." || body.Messages[1].Content != `hello "bot"` {
		t.Fatalf("messages = %+v", body.Messages)
	}
}

func TestClientCompleteWithoutSystemPrompt(t *testing.T) {
	client := New("secret", "deepseek-v4-flash", 500, 0)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body completionRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" {
			t.Fatalf("messages = %+v", body.Messages)
		}
		return response(http.StatusOK, `{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}]}`), nil
	})}

	if _, err := client.Complete(context.Background(), "", []Message{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestClientCompleteAPIError(t *testing.T) {
	client := New("secret", "deepseek-v4-flash", 500, 0)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`), nil
	})}

	_, err := client.Complete(context.Background(), "", []Message{{Role: RoleUser, Content: "hello"}})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusTooManyRequests || !strings.Contains(apiErr.Error(), "slow down") {
		t.Fatalf("Complete() error = %#v", err)
	}
}

func TestClientCompleteReturnsEmptyFilteredChoice(t *testing.T) {
	client := New("secret", "deepseek-v4-flash", 500, 0)
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"choices":[{"message":{"content":""},"finish_reason":"content_filter"}]}`), nil
	})}

	completion, err := client.Complete(context.Background(), "", []Message{{Role: RoleUser, Content: "hello"}})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completion.FinishReason != FinishContentFilter || completion.Text != "" {
		t.Fatalf("Complete() = %+v", completion)
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
