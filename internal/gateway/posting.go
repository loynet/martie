package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxPostingResponseBytes = 1 << 20

type PostingClient struct {
	baseURL     string
	integration string
	secret      string
	http        *http.Client
}

type ReplyRequest struct {
	Board    string
	ThreadID int64
	Message  string
	Sage     bool
}

type ReplyResponse struct {
	Board    string     `json:"board"`
	ThreadID int64      `json:"thread_id"`
	PostID   int64      `json:"post_id"`
	URL      string     `json:"url"`
	Origin   PostOrigin `json:"origin"`
}

type PostingError struct {
	StatusCode     int
	Code           string `json:"code"`
	Message        string `json:"message"`
	Retryable      bool   `json:"retryable"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
}

func (e *PostingError) Error() string {
	if e.Code == "" && e.Message == "" {
		return fmt.Sprintf("gateway posting status: %d", e.StatusCode)
	}
	return fmt.Sprintf("gateway posting status: %d: %s: %s", e.StatusCode, e.Code, e.Message)
}

func NewPostingClient(baseURL, integration, secret string, timeout time.Duration) *PostingClient {
	return &PostingClient{
		baseURL:     strings.TrimRight(baseURL, "/"),
		integration: strings.TrimSpace(integration),
		secret:      secret,
		http:        &http.Client{Timeout: timeout},
	}
}

func (c *PostingClient) Reply(ctx context.Context, request ReplyRequest) (ReplyResponse, error) {
	path := "/integration/v1/threads/" + url.PathEscape(request.Board) + "/" + strconv.FormatInt(request.ThreadID, 10) + "/replies"
	body, err := json.Marshal(replyRequestBody{
		Message: request.Message,
		Sage:    request.Sage,
	})
	if err != nil {
		return ReplyResponse{}, fmt.Errorf("encode gateway reply request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return ReplyResponse{}, fmt.Errorf("create gateway reply request: %w", err)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-ptchan-integration", c.integration)
	req.Header.Set("x-ptchan-timestamp", timestamp)
	req.Header.Set("x-ptchan-signature", integrationSignature(c.secret, timestamp, http.MethodPost, path, body))

	resp, err := c.http.Do(req)
	if err != nil {
		return ReplyResponse{}, fmt.Errorf("send gateway reply request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(&io.LimitedReader{R: resp.Body, N: maxThreadReadErrorBytes})
		if postingError, ok := decodePostingError(body); ok {
			postingError.StatusCode = resp.StatusCode
			return ReplyResponse{}, postingError
		}
		message := strings.TrimSpace(string(body))
		if message != "" {
			return ReplyResponse{}, fmt.Errorf("gateway posting status: %s: %s", resp.Status, message)
		}
		return ReplyResponse{}, fmt.Errorf("gateway posting status: %s", resp.Status)
	}

	bodyReader := &io.LimitedReader{R: resp.Body, N: maxPostingResponseBytes + 1}
	var reply ReplyResponse
	if err := json.NewDecoder(bodyReader).Decode(&reply); err != nil {
		if bodyReader.N == 0 {
			return ReplyResponse{}, fmt.Errorf("gateway posting response exceeds %d bytes", maxPostingResponseBytes)
		}
		return ReplyResponse{}, fmt.Errorf("decode gateway posting response: %w", err)
	}
	if bodyReader.N == 0 {
		return ReplyResponse{}, fmt.Errorf("gateway posting response exceeds %d bytes", maxPostingResponseBytes)
	}
	return reply, nil
}

type replyRequestBody struct {
	Message string `json:"message"`
	Sage    bool   `json:"sage"`
}

type postingErrorEnvelope struct {
	Error PostingError `json:"error"`
}

func decodePostingError(body []byte) (*PostingError, bool) {
	var envelope postingErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && (envelope.Error.Code != "" || envelope.Error.Message != "") {
		return &envelope.Error, true
	}

	var postingError PostingError
	if err := json.Unmarshal(body, &postingError); err == nil && (postingError.Code != "" || postingError.Message != "") {
		return &postingError, true
	}
	return nil, false
}
