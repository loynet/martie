package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const MaxWebhookClockSkew = 5 * time.Minute

var ErrWebhookAuthentication = errors.New("ptchan gateway webhook authentication failed")

func VerifyWebhookBody(secret, eventID, timestamp, gotSignature string, body []byte, now time.Time) (*WebhookEvent, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("%w: secret is empty", ErrWebhookAuthentication)
	}
	if eventID == "" || timestamp == "" || gotSignature == "" {
		return nil, fmt.Errorf("%w: missing signature headers", ErrWebhookAuthentication)
	}
	observed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return nil, fmt.Errorf("%w: bad timestamp: %v", ErrWebhookAuthentication, err)
	}
	if skew := now.Sub(observed); skew > MaxWebhookClockSkew || skew < -MaxWebhookClockSkew {
		return nil, fmt.Errorf("%w: timestamp is outside allowed skew", ErrWebhookAuthentication)
	}
	if !validSignature(gotSignature, webhookSignature(secret, timestamp, body)) {
		return nil, fmt.Errorf("%w: bad signature", ErrWebhookAuthentication)
	}

	event, err := decodeWebhookEvent(body)
	if err != nil {
		return nil, err
	}
	if event.EventID != eventID {
		return nil, fmt.Errorf("ptchan gateway webhook: event id mismatch")
	}
	return event, nil
}

func decodeWebhookEvent(body []byte) (*WebhookEvent, error) {
	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("ptchan gateway webhook: decode event: %w", err)
	}
	if event.EventID == "" {
		return nil, fmt.Errorf("ptchan gateway webhook: event_id is required")
	}
	if event.SchemaVersion != SchemaV1 {
		return nil, fmt.Errorf("ptchan gateway webhook: unsupported schema_version %q", event.SchemaVersion)
	}
	if event.Kind == "" {
		return nil, fmt.Errorf("ptchan gateway webhook: kind is required")
	}
	if event.Source == "" {
		return nil, fmt.Errorf("ptchan gateway webhook: source is required")
	}
	if event.ObservedAt.IsZero() {
		return nil, fmt.Errorf("ptchan gateway webhook: observed_at is required")
	}
	if err := validatePost(event.Post); err != nil {
		return nil, fmt.Errorf("ptchan gateway webhook: invalid post: %w", err)
	}
	return &event, nil
}
