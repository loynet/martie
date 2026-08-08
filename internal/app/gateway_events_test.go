package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/loynet/ptchan-gateway/clients/go"
)

func TestGatewayEventServerDispatchesEvent(t *testing.T) {
	body := []byte(`{
		"schema_version": "1",
		"event_id": "ptchan:post.created:i:101",
		"kind": "post.created",
		"source": "ptchan",
		"observed_at": "2026-07-20T12:00:00Z",
		"post": {"board": "i", "thread_id": 100, "post_id": 101, "url": "https://ptchan.test/i/thread/100.html#101", "date": "2026-07-20T12:00:00Z", "attachment_count": 0}
	}`)
	now := time.Now().UTC()
	consumer := &recordingGatewayEventConsumer{}
	server := gatewayEventServer{
		ptchan:  PtchanConfig{Secret: "secret"},
		consume: consumer.ConsumeGatewayEvent,
		logger:  discardLogger(),
		metrics: newMetrics(),
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/ptchan/events", bytes.NewReader(body))
	timestamp := now.Format(time.RFC3339Nano)
	request.Header.Set("x-ptchan-event-id", "ptchan:post.created:i:101")
	request.Header.Set("x-ptchan-timestamp", timestamp)
	request.Header.Set("x-ptchan-signature", webhookTestSignature("secret", timestamp, body))
	requestCtx, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(requestCtx)

	response := httptest.NewRecorder()
	server.handleEvent(context.Background(), response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if len(consumer.events) != 1 || consumer.events[0].EventID != "ptchan:post.created:i:101" || consumer.events[0].Post.PostID != 101 {
		t.Fatalf("events = %+v", consumer.events)
	}
	if consumer.ctxErr != nil {
		t.Fatalf("consumer context was canceled with request: %v", consumer.ctxErr)
	}
}

func TestGatewayEventServerClassifiesRejectedEvents(t *testing.T) {
	now := time.Now().UTC()
	metrics := newMetrics()
	server := gatewayEventServer{
		ptchan:  PtchanConfig{Secret: "secret"},
		logger:  discardLogger(),
		metrics: metrics,
	}
	body := []byte(`{"event_id":"event-1"}`)
	timestamp := now.Format(time.RFC3339)

	invalidEvent := httptest.NewRequest(http.MethodPost, gatewayWebhookPath, bytes.NewReader(body))
	invalidEvent.Header.Set("x-ptchan-event-id", "event-1")
	invalidEvent.Header.Set("x-ptchan-timestamp", timestamp)
	invalidEvent.Header.Set("x-ptchan-signature", webhookTestSignature("secret", timestamp, body))
	invalidResponse := httptest.NewRecorder()
	server.handleEvent(context.Background(), invalidResponse, invalidEvent)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid event status = %d, want 400", invalidResponse.Code)
	}

	unauthorized := httptest.NewRequest(http.MethodPost, gatewayWebhookPath, bytes.NewReader(body))
	unauthorized.Header.Set("x-ptchan-event-id", "event-1")
	unauthorized.Header.Set("x-ptchan-timestamp", timestamp)
	unauthorized.Header.Set("x-ptchan-signature", "hmac-sha256=bad")
	unauthorizedResponse := httptest.NewRecorder()
	server.handleEvent(context.Background(), unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorizedResponse.Code)
	}

	scrape := httptest.NewRecorder()
	metrics.handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, want := range []string{
		`martie_gateway_webhook_requests_total{result="invalid_event"} 1`,
		`martie_gateway_webhook_requests_total{result="unauthorized"} 1`,
	} {
		if !strings.Contains(scrape.Body.String(), want) {
			t.Fatalf("metrics missing %q:\n%s", want, scrape.Body.String())
		}
	}
}

func TestGatewayEventServerRejectsLargePayload(t *testing.T) {
	server := gatewayEventServer{
		ptchan:  PtchanConfig{Secret: "secret"},
		logger:  discardLogger(),
		metrics: newMetrics(),
	}
	request := httptest.NewRequest(http.MethodPost, gatewayWebhookPath, strings.NewReader(strings.Repeat("x", maxGatewayEventBytes+1)))
	response := httptest.NewRecorder()

	server.handleEvent(context.Background(), response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

type recordingGatewayEventConsumer struct {
	events []gateway.WebhookEvent
	ctxErr error
}

func (c *recordingGatewayEventConsumer) ConsumeGatewayEvent(ctx context.Context, event gateway.WebhookEvent) error {
	c.events = append(c.events, event)
	c.ctxErr = ctx.Err()
	return nil
}

func webhookTestSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil))
}
