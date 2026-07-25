package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPostingClientReplyUsesSignedGatewayEndpoint(t *testing.T) {
	var gotRequest *http.Request
	var gotBody []byte
	client := &PostingClient{
		baseURL:     "http://gateway.test",
		integration: "martie",
		secret:      "secret",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotRequest = req
			var err error
			gotBody, err = io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"board": "i",
					"thread_id": 100,
					"post_id": 105,
					"url": "https://ptchan.test/i/thread/100.html#105",
					"origin": {"kind": "integration", "name": "martie"}
				}`)),
			}, nil
		})},
	}

	reply, err := client.Reply(context.Background(), ReplyRequest{
		Board:    "i",
		ThreadID: 100,
		Message:  ">>101\nhello",
		Sage:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotRequest == nil {
		t.Fatal("request was not sent")
	}
	if gotRequest.Method != http.MethodPost || gotRequest.URL.Path != "/integration/v1/threads/i/100/replies" {
		t.Fatalf("request = %s %s", gotRequest.Method, gotRequest.URL.String())
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != ">>101\nhello" || body["sage"] != true {
		t.Fatalf("body = %s", string(gotBody))
	}
	timestamp := gotRequest.Header.Get("x-ptchan-timestamp")
	if gotRequest.Header.Get("Content-Type") != "application/json" || gotRequest.Header.Get("x-ptchan-integration") != "martie" || timestamp == "" {
		t.Fatalf("headers = %v", gotRequest.Header)
	}
	wantSignature := integrationSignature("secret", timestamp, http.MethodPost, "/integration/v1/threads/i/100/replies", gotBody)
	if got := gotRequest.Header.Get("x-ptchan-signature"); got != wantSignature {
		t.Fatalf("signature = %q, want %q", got, wantSignature)
	}
	if reply.PostID != 105 || reply.Origin.Name != "martie" {
		t.Fatalf("reply = %+v", reply)
	}
}

func TestPostingClientReplyReportsStructuredError(t *testing.T) {
	client := &PostingClient{
		baseURL:     "http://gateway.test",
		integration: "martie",
		secret:      "secret",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Status:     "429 Too Many Requests",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"rate_limited","message":"wait","retryable":true,"upstream_status":429}}`)),
			}, nil
		})},
	}

	_, err := client.Reply(context.Background(), ReplyRequest{Board: "i", ThreadID: 100, Message: "hello"})
	var postingErr *PostingError
	if !errors.As(err, &postingErr) {
		t.Fatalf("Reply() error = %T %v, want PostingError", err, err)
	}
	if postingErr.StatusCode != http.StatusTooManyRequests || postingErr.Code != "rate_limited" || !postingErr.Retryable || postingErr.UpstreamStatus != http.StatusTooManyRequests {
		t.Fatalf("posting error = %+v", postingErr)
	}
}

func TestPostingClientReplyReportsLegacyStructuredError(t *testing.T) {
	client := &PostingClient{
		baseURL:     "http://gateway.test",
		integration: "martie",
		secret:      "secret",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Status:     "502 Bad Gateway",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"code":"reply_state_unknown","message":"check thread","retryable":false}`)),
			}, nil
		})},
	}

	_, err := client.Reply(context.Background(), ReplyRequest{Board: "i", ThreadID: 100, Message: "hello"})
	var postingErr *PostingError
	if !errors.As(err, &postingErr) {
		t.Fatalf("Reply() error = %T %v, want PostingError", err, err)
	}
	if postingErr.StatusCode != http.StatusBadGateway || postingErr.Code != "reply_state_unknown" || postingErr.Retryable {
		t.Fatalf("posting error = %+v", postingErr)
	}
}
