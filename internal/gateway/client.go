package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	requestTimeout   = 15 * time.Second
	maxResponseBytes = 1 << 20
)

type Client struct {
	BaseURL     string
	Credentials Credentials
	HTTPClient  *http.Client
}

func NewClient(baseURL string, credentials Credentials) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Credentials: credentials,
		HTTPClient:  &http.Client{Timeout: requestTimeout},
	}
}

func (c *Client) ReadThread(ctx context.Context, ref ThreadRef, limit int) (*Thread, error) {
	path := ref.path()
	if limit > 0 {
		path = fmt.Sprintf("%s?limit=%d", path, limit)
	}

	var thread Thread
	if err := c.do(ctx, http.MethodGet, path, nil, &thread); err != nil {
		return nil, err
	}
	if err := validateThread(thread, ref); err != nil {
		return nil, fmt.Errorf("ptchan gateway: invalid thread response: %w", err)
	}
	return &thread, nil
}

func (c *Client) PostReply(ctx context.Context, ref ThreadRef, message string, sage bool) (*ReplyResponse, error) {
	body, err := json.Marshal(struct {
		Message string `json:"message"`
		Sage    bool   `json:"sage"`
	}{
		Message: message,
		Sage:    sage,
	})
	if err != nil {
		return nil, err
	}

	var reply ReplyResponse
	if err := c.do(ctx, http.MethodPost, ref.path()+"/replies", body, &reply); err != nil {
		return nil, err
	}
	if err := validateReplyResponse(reply, ref, c.Credentials.Name); err != nil {
		return nil, fmt.Errorf("ptchan gateway: invalid reply response: %w", err)
	}
	return &reply, nil
}

type Error struct {
	StatusCode     int    `json:"-"`
	Code           string `json:"code"`
	Message        string `json:"message"`
	Retryable      bool   `json:"retryable"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
	Body           []byte `json:"-"`
}

func (e *Error) Error() string {
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("ptchan gateway: status %d: %s: %s", e.StatusCode, e.Code, e.Message)
	}
	if e.Code != "" {
		return fmt.Sprintf("ptchan gateway: status %d: %s", e.StatusCode, e.Code)
	}
	return fmt.Sprintf("ptchan gateway: status %d", e.StatusCode)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	c.sign(req, path, body)

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(respBody) > maxResponseBytes {
		return fmt.Errorf("ptchan gateway: response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeError(resp.StatusCode, respBody)
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (c *Client) sign(req *http.Request, path string, body []byte) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	req.Header.Set(headerIntegration, strings.TrimSpace(c.Credentials.Name))
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerSignature, signature(c.Credentials.Secret, timestamp, req.Method, path, body))
}

func decodeError(statusCode int, body []byte) error {
	err := &Error{
		StatusCode: statusCode,
		Body:       append([]byte(nil), body...),
	}

	var envelope struct {
		Error *Error `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		err.Code = envelope.Error.Code
		err.Message = envelope.Error.Message
		err.Retryable = envelope.Error.Retryable
		err.UpstreamStatus = envelope.Error.UpstreamStatus
		return err
	}
	return err
}

func validateThread(thread Thread, requested ThreadRef) error {
	if thread.Board == "" || thread.ThreadID <= 0 {
		return fmt.Errorf("board and thread_id are required")
	}
	if thread.ThreadRef() != requested {
		return fmt.Errorf("coordinates do not match requested thread")
	}
	for i, post := range thread.Posts {
		if err := validatePost(post); err != nil {
			return fmt.Errorf("posts[%d]: %w", i, err)
		}
		if post.ThreadRef() != requested {
			return fmt.Errorf("posts[%d]: coordinates do not match requested thread", i)
		}
		if i > 0 && post.Date.Before(thread.Posts[i-1].Date) {
			return fmt.Errorf("posts are not chronological")
		}
	}
	return nil
}

func validateReplyResponse(reply ReplyResponse, requested ThreadRef, integrationName string) error {
	if reply.Board == "" || reply.ThreadID <= 0 || reply.PostID <= 0 || reply.URL == "" {
		return fmt.Errorf("board, thread_id, post_id, and url are required")
	}
	if reply.Board != requested.Board || reply.ThreadID != requested.ThreadID {
		return fmt.Errorf("coordinates do not match requested thread")
	}
	if err := validateOrigin(reply.Origin); err != nil {
		return fmt.Errorf("origin: %w", err)
	}
	if reply.Origin.Name != strings.TrimSpace(integrationName) {
		return fmt.Errorf("origin does not match requesting integration")
	}
	return nil
}

func validatePost(post Post) error {
	if post.Board == "" || post.ThreadID <= 0 || post.PostID <= 0 || post.URL == "" || post.Date.IsZero() {
		return fmt.Errorf("board, thread_id, post_id, url, and date are required")
	}
	if post.AttachmentCount < 0 {
		return fmt.Errorf("attachment_count must be non-negative")
	}
	if post.Origin != nil {
		if err := validateOrigin(*post.Origin); err != nil {
			return fmt.Errorf("origin: %w", err)
		}
	}
	for _, ref := range post.References {
		if ref.Board == "" || ref.ThreadID <= 0 || ref.PostID <= 0 {
			return fmt.Errorf("post references require board, thread_id, and post_id")
		}
	}
	for _, ref := range post.ReferencedBy {
		if ref.Board == "" || ref.ThreadID <= 0 || ref.PostID <= 0 {
			return fmt.Errorf("post references require board, thread_id, and post_id")
		}
	}
	return nil
}

func validateOrigin(origin PostOrigin) error {
	if origin.Kind != IntegrationOrigin || origin.Name == "" {
		return fmt.Errorf("kind must be %q and name is required", IntegrationOrigin)
	}
	return nil
}
