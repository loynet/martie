package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	KindThreadCreated Kind = "thread.created"
	KindPostCreated   Kind = "post.created"

	maxClockSkew = 5 * time.Minute
)

type Kind string

type Event struct {
	EventID    string    `json:"event_id"`
	Kind       Kind      `json:"kind"`
	Source     string    `json:"source"`
	ObservedAt time.Time `json:"observed_at"`
	Post       Post      `json:"post"`
}

type Post struct {
	Board             string      `json:"board"`
	ThreadID          int64       `json:"thread_id"`
	PostID            int64       `json:"post_id"`
	URL               string      `json:"url"`
	Date              time.Time   `json:"date"`
	Subject           string      `json:"subject"`
	Message           string      `json:"message"`
	Name              string      `json:"name"`
	Tripcode          string      `json:"tripcode"`
	Capcode           string      `json:"capcode"`
	Donor             *bool       `json:"donor"`
	Country           string      `json:"country"`
	PosterFingerprint string      `json:"poster_fingerprint"`
	Origin            *PostOrigin `json:"origin"`
	AttachmentCount   int         `json:"attachment_count"`
	References        []PostRef   `json:"references"`
	ReferencedBy      []PostRef   `json:"referenced_by"`
}

type PostOrigin struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type PostRef struct {
	Board    string `json:"board"`
	ThreadID int64  `json:"thread_id"`
	PostID   int64  `json:"post_id"`
}

func DecodeEvent(body []byte) (Event, error) {
	var event Event
	if err := json.Unmarshal(body, &event); err != nil {
		return Event{}, fmt.Errorf("decode gateway event: %w", err)
	}
	if event.EventID == "" {
		return Event{}, fmt.Errorf("gateway event_id is required")
	}
	if event.Kind != KindThreadCreated && event.Kind != KindPostCreated {
		return Event{}, fmt.Errorf("unsupported gateway event kind %q", event.Kind)
	}
	if event.ObservedAt.IsZero() {
		return Event{}, fmt.Errorf("gateway observed_at is required")
	}
	if event.Post.Board == "" || event.Post.ThreadID <= 0 || event.Post.PostID <= 0 {
		return Event{}, fmt.Errorf("gateway event post coordinates are required")
	}
	return event, nil
}

func VerifyWebhook(secret, timestamp, signature string, body []byte, now time.Time) error {
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("gateway webhook secret is empty")
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return fmt.Errorf("gateway timestamp must be RFC3339: %w", err)
	}
	if skew := now.Sub(parsed); skew > maxClockSkew || skew < -maxClockSkew {
		return fmt.Errorf("gateway timestamp is outside allowed skew")
	}

	provided, err := signatureBytes(signature)
	if err != nil {
		return err
	}
	expected := webhookSignature(secret, timestamp, body)
	if !hmac.Equal(provided, expected) {
		return fmt.Errorf("gateway signature mismatch")
	}
	return nil
}

func signatureBytes(signature string) ([]byte, error) {
	value, ok := strings.CutPrefix(signature, "hmac-sha256=")
	if !ok {
		return nil, fmt.Errorf("gateway signature must use hmac-sha256")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("gateway signature is not hex: %w", err)
	}
	return decoded, nil
}

func webhookSignature(secret, timestamp string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return mac.Sum(nil)
}
