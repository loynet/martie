package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignatureCoversReadPathAndQuery(t *testing.T) {
	got := signature("secret", "2026-07-19T12:00:00Z", "GET", "/integration/v1/threads/i/100?limit=50", nil)

	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte("2026-07-19T12:00:00Z.GET./integration/v1/threads/i/100?limit=50"))
	want := "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Fatalf("signature mismatch\nwant %s\n got %s", want, got)
	}
}

func TestSignatureCoversPostBody(t *testing.T) {
	body := []byte(`{"message":"hello","sage":false}`)
	got := signature("secret", "2026-07-19T12:00:00Z", "POST", "/integration/v1/threads/i/100/replies", body)

	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte("2026-07-19T12:00:00Z.POST./integration/v1/threads/i/100/replies."))
	mac.Write(body)
	want := "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Fatalf("signature mismatch\nwant %s\n got %s", want, got)
	}
}

func TestClientReadThreadUsesSignedGatewayEndpoint(t *testing.T) {
	var gotRequest *http.Request
	client := &Client{
		BaseURL:     "http://gateway.test",
		Credentials: Credentials{Name: "martie", Secret: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotRequest = req
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"board": "i",
					"thread_id": 100,
					"truncated": true,
					"posts": [
						{"board": "i", "thread_id": 100, "post_id": 100, "url": "https://ptchan.test/i/thread/100.html#100", "date": "2026-07-19T12:00:00Z", "message": "op"},
						{"board": "i", "thread_id": 100, "post_id": 101, "url": "https://ptchan.test/i/thread/100.html#101", "date": "2026-07-19T12:01:00Z", "message": "reply", "origin": {"kind": "integration", "name": "martie"}, "references": [{"board": "i", "thread_id": 100, "post_id": 100}]}
					]
				}`)),
			}, nil
		})},
	}

	thread, err := client.ReadThread(context.Background(), ThreadRef{Board: "i", ThreadID: 100}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if gotRequest == nil {
		t.Fatal("request was not sent")
	}
	if gotRequest.URL.Path != "/integration/v1/threads/i/100" || gotRequest.URL.RawQuery != "limit=3" {
		t.Fatalf("url = %s", gotRequest.URL.String())
	}
	timestamp := gotRequest.Header.Get("x-ptchan-timestamp")
	if gotRequest.Header.Get("x-ptchan-integration") != "martie" || timestamp == "" || gotRequest.Header.Get("x-ptchan-signature") == "" {
		t.Fatalf("missing signed thread read headers: %v", gotRequest.Header)
	}
	wantSignature := signature("secret", timestamp, http.MethodGet, "/integration/v1/threads/i/100?limit=3", nil)
	if got := gotRequest.Header.Get("x-ptchan-signature"); got != wantSignature {
		t.Fatalf("signature = %q, want %q", got, wantSignature)
	}
	if thread.Board != "i" || thread.ThreadID != 100 || !thread.Truncated || len(thread.Posts) != 2 || thread.Posts[0].Message != "op" || thread.Posts[1].Origin == nil || thread.Posts[1].Origin.Name != "martie" || len(thread.Posts[1].References) != 1 {
		t.Fatalf("thread = %+v", thread)
	}
}

func TestClientPostReplyUsesSignedGatewayEndpoint(t *testing.T) {
	var gotRequest *http.Request
	var gotBody []byte
	client := &Client{
		BaseURL:     "http://gateway.test",
		Credentials: Credentials{Name: "martie", Secret: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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

	reply, err := client.PostReply(context.Background(), ThreadRef{Board: "i", ThreadID: 100}, ">>101\nhello", true)
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
	wantSignature := signature("secret", timestamp, http.MethodPost, "/integration/v1/threads/i/100/replies", gotBody)
	if got := gotRequest.Header.Get("x-ptchan-signature"); got != wantSignature {
		t.Fatalf("signature = %q, want %q", got, wantSignature)
	}
	if reply.PostID != 105 || reply.Origin.Name != "martie" {
		t.Fatalf("reply = %+v", reply)
	}
}

func TestDecodeGatewayError(t *testing.T) {
	err := decodeError(429, []byte(`{"error":{"code":"rate_limited","message":"slow down","retryable":true,"upstream_status":429}}`))

	gatewayErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if gatewayErr.Code != "rate_limited" {
		t.Fatalf("code = %q", gatewayErr.Code)
	}
	if !gatewayErr.Retryable {
		t.Fatal("retryable = false")
	}
	if gatewayErr.UpstreamStatus != 429 {
		t.Fatalf("upstream status = %d", gatewayErr.UpstreamStatus)
	}
}

func TestDecodeGatewayLegacyError(t *testing.T) {
	err := decodeError(http.StatusBadGateway, []byte(`{"code":"reply_state_unknown","message":"check thread","retryable":false}`))

	gatewayErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if gatewayErr.Code != "reply_state_unknown" || gatewayErr.Retryable {
		t.Fatalf("gateway error = %+v", gatewayErr)
	}
}

func TestClientPostReplyRequiresCoordinates(t *testing.T) {
	client := &Client{
		BaseURL:     "http://gateway.test",
		Credentials: Credentials{Name: "martie", Secret: "secret"},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"board":"i","thread_id":100}`)),
			}, nil
		})},
	}

	if _, err := client.PostReply(context.Background(), ThreadRef{Board: "i", ThreadID: 100}, "hello", false); err == nil || !strings.Contains(err.Error(), "coordinates") {
		t.Fatalf("PostReply() error = %v, want coordinates error", err)
	}
}

func TestClientCheckReachableUsesHealthEndpoint(t *testing.T) {
	var gotPath string
	client := &Client{
		BaseURL: "http://gateway.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		})},
	}

	if err := client.CheckReachable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/healthz" {
		t.Fatalf("health path = %q, want /healthz", gotPath)
	}
}

func TestClientCheckReachableReportsRequestFailure(t *testing.T) {
	client := &Client{
		BaseURL: "http://gateway.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("lookup failed")
		})},
	}

	if err := client.CheckReachable(context.Background()); err == nil || !strings.Contains(err.Error(), "lookup failed") {
		t.Fatalf("CheckReachable() error = %v, want send error", err)
	}
}

func TestDecodeWebhookEvent(t *testing.T) {
	event, err := DecodeWebhookEvent([]byte(`{
		"event_id": "ptchan:post.created:i:101",
		"kind": "post.created",
		"source": "ptchan",
		"observed_at": "2026-07-19T12:00:01Z",
		"post": {
			"board": "i",
			"thread_id": 100,
			"post_id": 101,
			"url": "https://ptchan.test/i/thread/100.html#101",
			"date": "2026-07-19T12:00:00Z",
			"message": "reply",
			"origin": {"kind": "integration", "name": "martie"},
			"attachment_count": 1,
			"references": [{"board": "i", "thread_id": 100, "post_id": 100}]
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != PostCreated || event.Post.Board != "i" || event.Post.PostID != 101 || event.Post.Origin == nil || event.Post.Origin.Name != "martie" || len(event.Post.References) != 1 {
		t.Fatalf("event = %+v", event)
	}
}

func TestVerifyWebhookBody(t *testing.T) {
	body := []byte(`{"event_id":"ptchan:thread.created:i:100","kind":"thread.created","source":"ptchan","observed_at":"2026-07-19T12:00:00Z","post":{"board":"i","thread_id":100,"post_id":100}}`)
	timestamp := "2026-07-19T12:00:00Z"
	signature := webhookSignature("secret", timestamp, body)
	now := time.Date(2026, time.July, 19, 12, 0, 30, 0, time.UTC)

	if _, err := VerifyWebhookBody("secret", "ptchan:thread.created:i:100", timestamp, signature, body, now); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyWebhookBody("secret", "ptchan:thread.created:i:100", timestamp, signature, []byte(`{}`), now); err == nil || !strings.Contains(err.Error(), "bad signature") {
		t.Fatalf("bad body error = %v", err)
	}
	if _, err := VerifyWebhookBody("secret", "ptchan:thread.created:i:100", timestamp, signature, body, now.Add(10*time.Minute)); err == nil || !strings.Contains(err.Error(), "skew") {
		t.Fatalf("old timestamp error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
