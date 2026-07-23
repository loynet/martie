package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestContextClientFetchThreadUsesSignedGatewayEndpoint(t *testing.T) {
	var gotRequest *http.Request
	client := &ContextClient{
		baseURL:  "http://gateway.test",
		consumer: "martie",
		secret:   "secret",
		limit:    3,
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
						{"board": "i", "thread_id": 100, "post_id": 101, "url": "https://ptchan.test/i/thread/100.html#101", "date": "2026-07-19T12:01:00Z", "message": "reply", "references": [{"board": "i", "thread_id": 100, "post_id": 100}]}
					]
				}`)),
			}, nil
		})},
	}

	thread, err := client.FetchThread(context.Background(), "i", 100)
	if err != nil {
		t.Fatal(err)
	}
	if gotRequest == nil {
		t.Fatal("request was not sent")
	}
	if gotRequest.URL.Path != "/consumer/v1/threads/i/100" || gotRequest.URL.RawQuery != "limit=3" {
		t.Fatalf("url = %s", gotRequest.URL.String())
	}
	if gotRequest.Header.Get("x-ptchan-consumer") != "martie" || gotRequest.Header.Get("x-ptchan-timestamp") == "" || gotRequest.Header.Get("x-ptchan-signature") == "" {
		t.Fatalf("missing signed context headers: %v", gotRequest.Header)
	}
	if thread.Board != "i" || thread.ThreadID != 100 || !thread.Truncated || len(thread.Posts) != 2 || thread.Posts[0].Message != "op" || len(thread.Posts[1].References) != 1 {
		t.Fatalf("thread = %+v", thread)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
