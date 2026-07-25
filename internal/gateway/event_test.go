package gateway

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestDecodeEvent(t *testing.T) {
	event, err := DecodeEvent([]byte(`{
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
	if event.Kind != KindPostCreated || event.Post.Board != "i" || event.Post.PostID != 101 || event.Post.Origin == nil || event.Post.Origin.Name != "martie" || len(event.Post.References) != 1 {
		t.Fatalf("event = %+v", event)
	}
}

func TestDecodeEventRequiresObservedAt(t *testing.T) {
	_, err := DecodeEvent([]byte(`{
		"event_id": "ptchan:post.created:i:101",
		"kind": "post.created",
		"source": "ptchan",
		"post": {"board": "i", "thread_id": 100, "post_id": 101}
	}`))
	if err == nil || !strings.Contains(err.Error(), "observed_at") {
		t.Fatalf("DecodeEvent() error = %v, want observed_at error", err)
	}
}

func TestVerifyWebhook(t *testing.T) {
	body := []byte(`{"event_id":"ptchan:thread.created:i:100","kind":"thread.created","post":{"board":"i","thread_id":100,"post_id":100}}`)
	timestamp := "2026-07-19T12:00:00Z"
	signature := "hmac-sha256=" + hex.EncodeToString(webhookSignature("secret", timestamp, body))
	now := time.Date(2026, time.July, 19, 12, 0, 30, 0, time.UTC)

	if err := VerifyWebhook("secret", timestamp, signature, body, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyWebhook("secret", timestamp, signature, []byte(`{}`), now); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("bad body error = %v", err)
	}
	if err := VerifyWebhook("secret", timestamp, signature, body, now.Add(10*time.Minute)); err == nil || !strings.Contains(err.Error(), "skew") {
		t.Fatalf("old timestamp error = %v", err)
	}
}
