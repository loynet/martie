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

type Client struct {
	BaseURL     string
	Credentials Credentials
	HTTPClient  *http.Client
}

func NewClient(baseURL string, credentials Credentials, timeout time.Duration) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Credentials: credentials,
		HTTPClient:  &http.Client{Timeout: timeout},
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
	if reply.Board == "" || reply.ThreadID <= 0 || reply.PostID <= 0 {
		return nil, fmt.Errorf("ptchan gateway: reply response is missing coordinates")
	}
	return &reply, nil
}

func (c *Client) CheckReachable(ctx context.Context) error {
	if err := c.do(ctx, http.MethodGet, "/healthz", nil, nil); err != nil {
		return fmt.Errorf("check ptchan gateway health: %w", err)
	}
	return nil
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
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
	var legacy Error
	if json.Unmarshal(body, &legacy) == nil {
		err.Code = legacy.Code
		err.Message = legacy.Message
		err.Retryable = legacy.Retryable
		err.UpstreamStatus = legacy.UpstreamStatus
	}
	return err
}
