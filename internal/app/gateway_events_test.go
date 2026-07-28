package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"martie/internal/gateway"
)

func TestGatewayEventServerDispatchesToConsumers(t *testing.T) {
	body := []byte(`{
		"event_id": "ptchan:post.created:i:101",
		"kind": "post.created",
		"source": "ptchan",
		"observed_at": "2026-07-20T12:00:00Z",
		"post": {"board": "i", "thread_id": 100, "post_id": 101}
	}`)
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	first := &recordingGatewayEventConsumer{}
	second := &recordingGatewayEventConsumer{}
	server := gatewayEventServer{
		ptchan: PtchanConfig{Secret: "secret"},
		consumers: []gatewayEventTarget{
			{name: workerThreadNotifier, consumer: first},
			{name: workerChanner, consumer: second},
		},
		logger:  discardLogger(),
		metrics: newMetrics(),
		nowFunc: func() time.Time { return now },
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/ptchan/events", bytes.NewReader(body))
	timestamp := now.Format(time.RFC3339Nano)
	request.Header.Set("x-ptchan-event-id", "ptchan:post.created:i:101")
	request.Header.Set("x-ptchan-timestamp", timestamp)
	request.Header.Set("x-ptchan-signature", webhookTestSignature("secret", timestamp, body))

	response := httptest.NewRecorder()
	server.handleEvent(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if len(first.events) != 1 || len(second.events) != 1 || first.events[0].EventID != "ptchan:post.created:i:101" || second.events[0].Post.PostID != 101 {
		t.Fatalf("events = %+v %+v", first.events, second.events)
	}
}

type recordingGatewayEventConsumer struct {
	events []gateway.WebhookEvent
}

func (c *recordingGatewayEventConsumer) ConsumeGatewayEvent(_ context.Context, event gateway.WebhookEvent) error {
	c.events = append(c.events, event)
	return nil
}

func webhookTestSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil))
}
