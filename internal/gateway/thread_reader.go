package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxThreadReadResponseBytes = 4 << 20
const maxThreadReadErrorBytes = 512

type ThreadReader struct {
	baseURL     string
	integration string
	secret      string
	limit       int
	http        *http.Client
}

func NewThreadReader(baseURL, integration, secret string, timeout time.Duration, limit int) *ThreadReader {
	return &ThreadReader{
		baseURL:     strings.TrimRight(baseURL, "/"),
		integration: strings.TrimSpace(integration),
		secret:      secret,
		limit:       limit,
		http:        &http.Client{Timeout: timeout},
	}
}

func (c *ThreadReader) ReadThread(ctx context.Context, board string, threadID int64) (Thread, error) {
	path := "/integration/v1/threads/" + url.PathEscape(board) + "/" + strconv.FormatInt(threadID, 10)
	if c.limit > 0 {
		path += "?limit=" + strconv.Itoa(c.limit)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return Thread{}, fmt.Errorf("create gateway thread read request: %w", err)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set("x-ptchan-integration", c.integration)
	req.Header.Set("x-ptchan-timestamp", timestamp)
	req.Header.Set("x-ptchan-signature", integrationSignature(c.secret, timestamp, http.MethodGet, path))

	resp, err := c.http.Do(req)
	if err != nil {
		return Thread{}, fmt.Errorf("send gateway thread read request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(&io.LimitedReader{R: resp.Body, N: maxThreadReadErrorBytes})
		message := strings.TrimSpace(string(body))
		if message != "" {
			return Thread{}, fmt.Errorf("gateway thread read status: %s: %s", resp.Status, message)
		}
		return Thread{}, fmt.Errorf("gateway thread read status: %s", resp.Status)
	}

	body := &io.LimitedReader{R: resp.Body, N: maxThreadReadResponseBytes + 1}
	var thread Thread
	if err := json.NewDecoder(body).Decode(&thread); err != nil {
		if body.N == 0 {
			return Thread{}, fmt.Errorf("gateway thread read response exceeds %d bytes", maxThreadReadResponseBytes)
		}
		return Thread{}, fmt.Errorf("decode gateway thread read response: %w", err)
	}
	if body.N == 0 {
		return Thread{}, fmt.Errorf("gateway thread read response exceeds %d bytes", maxThreadReadResponseBytes)
	}
	return thread, nil
}

func (c *ThreadReader) CheckReachable(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("create gateway health request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send gateway health request: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

type Thread struct {
	Board     string `json:"board"`
	ThreadID  int64  `json:"thread_id"`
	Posts     []Post `json:"posts"`
	Truncated bool   `json:"truncated"`
}

func integrationSignature(secret, timestamp, method, path string, body ...[]byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write([]byte(method))
	mac.Write([]byte("."))
	mac.Write([]byte(path))
	if len(body) > 0 {
		mac.Write([]byte("."))
		mac.Write(body[0])
	}
	return "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil))
}
