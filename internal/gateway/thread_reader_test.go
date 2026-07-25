package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestThreadReaderReadThreadUsesSignedGatewayEndpoint(t *testing.T) {
	var gotRequest *http.Request
	client := &ThreadReader{
		baseURL:     "http://gateway.test",
		integration: "martie",
		secret:      "secret",
		limit:       3,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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

	thread, err := client.ReadThread(context.Background(), "i", 100)
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
	wantSignature := integrationSignature("secret", timestamp, http.MethodGet, "/integration/v1/threads/i/100?limit=3")
	if got := gotRequest.Header.Get("x-ptchan-signature"); got != wantSignature {
		t.Fatalf("signature = %q, want %q", got, wantSignature)
	}
	if thread.Board != "i" || thread.ThreadID != 100 || !thread.Truncated || len(thread.Posts) != 2 || thread.Posts[0].Message != "op" || thread.Posts[1].Origin == nil || thread.Posts[1].Origin.Name != "martie" || len(thread.Posts[1].References) != 1 {
		t.Fatalf("thread = %+v", thread)
	}
}

func TestThreadReaderReadThreadIncludesStatusBody(t *testing.T) {
	client := &ThreadReader{
		baseURL:     "http://gateway.test",
		integration: "martie",
		secret:      "secret",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Status:     "403 Forbidden",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("board blocked by policy")),
			}, nil
		})},
	}

	_, err := client.ReadThread(context.Background(), "i", 100)
	if err == nil || !strings.Contains(err.Error(), "403 Forbidden: board blocked by policy") {
		t.Fatalf("ReadThread() error = %v, want status body", err)
	}
}

func TestThreadReaderCheckReachableUsesHealthEndpoint(t *testing.T) {
	var gotPath string
	client := &ThreadReader{
		baseURL: "http://gateway.test",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("not found")),
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

func TestThreadReaderCheckReachableReportsRequestFailure(t *testing.T) {
	client := &ThreadReader{
		baseURL: "http://gateway.test",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("lookup failed")
		})},
	}

	if err := client.CheckReachable(context.Background()); err == nil || !strings.Contains(err.Error(), "send gateway health request") {
		t.Fatalf("CheckReachable() error = %v, want send error", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
