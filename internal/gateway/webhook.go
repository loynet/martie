package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const MaxWebhookClockSkew = 5 * time.Minute

func VerifyWebhookBody(secret, eventID, timestamp, gotSignature string, body []byte, now time.Time) (*WebhookEvent, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("ptchan gateway webhook: secret is empty")
	}
	if eventID == "" || timestamp == "" || gotSignature == "" {
		return nil, fmt.Errorf("ptchan gateway webhook: missing signature headers")
	}
	observed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return nil, fmt.Errorf("ptchan gateway webhook: bad timestamp: %w", err)
	}
	if skew := now.Sub(observed); skew > MaxWebhookClockSkew || skew < -MaxWebhookClockSkew {
		return nil, fmt.Errorf("ptchan gateway webhook: timestamp is outside allowed skew")
	}
	if !validSignature(gotSignature, webhookSignature(secret, timestamp, body)) {
		return nil, fmt.Errorf("ptchan gateway webhook: bad signature")
	}

	event, err := DecodeWebhookEvent(body)
	if err != nil {
		return nil, err
	}
	if event.EventID != eventID {
		return nil, fmt.Errorf("ptchan gateway webhook: event id mismatch")
	}
	return event, nil
}

func VerifyWebhook(r *http.Request, secret string, now time.Time) (*WebhookEvent, error) {
	body, err := readWebhookBody(r)
	if err != nil {
		return nil, err
	}
	return VerifyWebhookBody(secret, r.Header.Get(headerEventID), r.Header.Get(headerTimestamp), r.Header.Get(headerSignature), body, now)
}

func readWebhookBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("ptchan gateway webhook: read body: %w", err)
	}
	return body, nil
}

func DecodeWebhookEvent(body []byte) (*WebhookEvent, error) {
	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("ptchan gateway webhook: decode event: %w", err)
	}
	if event.EventID == "" {
		return nil, fmt.Errorf("ptchan gateway webhook: event_id is required")
	}
	if event.Kind != ThreadCreated && event.Kind != PostCreated {
		return nil, fmt.Errorf("ptchan gateway webhook: unsupported event kind %q", event.Kind)
	}
	if event.ObservedAt.IsZero() {
		return nil, fmt.Errorf("ptchan gateway webhook: observed_at is required")
	}
	if event.Post.Board == "" || event.Post.ThreadID <= 0 || event.Post.PostID <= 0 {
		return nil, fmt.Errorf("ptchan gateway webhook: post coordinates are required")
	}
	return &event, nil
}
