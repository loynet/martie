package gateway

import (
	"fmt"
	"net/url"
	"time"
)

const (
	headerIntegration = "x-ptchan-integration"
	headerEventID     = "x-ptchan-event-id"
	headerTimestamp   = "x-ptchan-timestamp"
	headerSignature   = "x-ptchan-signature"
)

type EventKind string

const (
	ThreadCreated EventKind = "thread.created"
	PostCreated   EventKind = "post.created"
)

type Credentials struct {
	Name   string
	Secret string
}

type ThreadRef struct {
	Board    string
	ThreadID int64
}

func (r ThreadRef) path() string {
	return fmt.Sprintf("/integration/v1/threads/%s/%d", url.PathEscape(r.Board), r.ThreadID)
}

type WebhookEvent struct {
	EventID    string    `json:"event_id"`
	Kind       EventKind `json:"kind"`
	Source     string    `json:"source"`
	ObservedAt time.Time `json:"observed_at"`
	Post       Post      `json:"post"`
}

type ReplyResponse struct {
	Board    string     `json:"board"`
	ThreadID int64      `json:"thread_id"`
	PostID   int64      `json:"post_id"`
	URL      string     `json:"url"`
	Origin   PostOrigin `json:"origin"`
}

func (r ReplyResponse) ThreadRef() ThreadRef {
	return ThreadRef{Board: r.Board, ThreadID: r.ThreadID}
}

type Thread struct {
	Board     string `json:"board"`
	ThreadID  int64  `json:"thread_id"`
	Posts     []Post `json:"posts"`
	Truncated bool   `json:"truncated"`
}

func (t Thread) ThreadRef() ThreadRef {
	return ThreadRef{Board: t.Board, ThreadID: t.ThreadID}
}

type Post struct {
	Board             string      `json:"board"`
	ThreadID          int64       `json:"thread_id"`
	PostID            int64       `json:"post_id"`
	URL               string      `json:"url"`
	Date              time.Time   `json:"date"`
	Subject           string      `json:"subject,omitempty"`
	Message           string      `json:"message,omitempty"`
	Name              string      `json:"name,omitempty"`
	Tripcode          string      `json:"tripcode,omitempty"`
	Capcode           string      `json:"capcode,omitempty"`
	Donor             *bool       `json:"donor,omitempty"`
	Country           string      `json:"country,omitempty"`
	PosterFingerprint string      `json:"poster_fingerprint,omitempty"`
	Origin            *PostOrigin `json:"origin,omitempty"`
	AttachmentCount   int         `json:"attachment_count"`
	References        []PostRef   `json:"references,omitempty"`
	ReferencedBy      []PostRef   `json:"referenced_by,omitempty"`
}

func (p Post) ThreadRef() ThreadRef {
	return ThreadRef{Board: p.Board, ThreadID: p.ThreadID}
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

func (r PostRef) ThreadRef() ThreadRef {
	return ThreadRef{Board: r.Board, ThreadID: r.ThreadID}
}
