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

const maxContextResponseBytes = 4 << 20

type ContextClient struct {
	baseURL  string
	consumer string
	secret   string
	limit    int
	http     *http.Client
}

func NewContextClient(baseURL, consumer, secret string, timeout time.Duration, limit int) *ContextClient {
	return &ContextClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		consumer: strings.TrimSpace(consumer),
		secret:   secret,
		limit:    limit,
		http:     &http.Client{Timeout: timeout},
	}
}

func (c *ContextClient) FetchThread(ctx context.Context, board string, threadID int64) (Thread, error) {
	path := "/consumer/v1/threads/" + url.PathEscape(board) + "/" + strconv.FormatInt(threadID, 10)
	if c.limit > 0 {
		path += "?limit=" + strconv.Itoa(c.limit)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return Thread{}, fmt.Errorf("create gateway context request: %w", err)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set("x-ptchan-consumer", c.consumer)
	req.Header.Set("x-ptchan-timestamp", timestamp)
	req.Header.Set("x-ptchan-signature", contextSignature(c.secret, timestamp, http.MethodGet, path))

	resp, err := c.http.Do(req)
	if err != nil {
		return Thread{}, fmt.Errorf("send gateway context request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Thread{}, fmt.Errorf("gateway context status: %s", resp.Status)
	}

	body := &io.LimitedReader{R: resp.Body, N: maxContextResponseBytes + 1}
	var thread Thread
	if err := json.NewDecoder(body).Decode(&thread); err != nil {
		if body.N == 0 {
			return Thread{}, fmt.Errorf("gateway context response exceeds %d bytes", maxContextResponseBytes)
		}
		return Thread{}, fmt.Errorf("decode gateway context response: %w", err)
	}
	if body.N == 0 {
		return Thread{}, fmt.Errorf("gateway context response exceeds %d bytes", maxContextResponseBytes)
	}
	return thread, nil
}

type Thread struct {
	Board     string `json:"board"`
	ThreadID  int64  `json:"thread_id"`
	Posts     []Post `json:"posts"`
	Truncated bool   `json:"truncated"`
}

func contextSignature(secret, timestamp, method, path string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write([]byte(method))
	mac.Write([]byte("."))
	mac.Write([]byte(path))
	return "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil))
}
