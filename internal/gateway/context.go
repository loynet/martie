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

	"martie/internal/ptchan"
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

func (c *ContextClient) FetchThread(ctx context.Context, board string, threadID int64) (ptchan.Thread, error) {
	path := "/consumer/v1/threads/" + url.PathEscape(board) + "/" + strconv.FormatInt(threadID, 10)
	if c.limit > 0 {
		path += "?limit=" + strconv.Itoa(c.limit)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return ptchan.Thread{}, fmt.Errorf("create gateway context request: %w", err)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set("x-ptchan-consumer", c.consumer)
	req.Header.Set("x-ptchan-timestamp", timestamp)
	req.Header.Set("x-ptchan-signature", contextSignature(c.secret, timestamp, http.MethodGet, path))

	resp, err := c.http.Do(req)
	if err != nil {
		return ptchan.Thread{}, fmt.Errorf("send gateway context request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ptchan.Thread{}, fmt.Errorf("gateway context status: %s", resp.Status)
	}

	body := &io.LimitedReader{R: resp.Body, N: maxContextResponseBytes + 1}
	var thread contextThread
	if err := json.NewDecoder(body).Decode(&thread); err != nil {
		if body.N == 0 {
			return ptchan.Thread{}, fmt.Errorf("gateway context response exceeds %d bytes", maxContextResponseBytes)
		}
		return ptchan.Thread{}, fmt.Errorf("decode gateway context response: %w", err)
	}
	if body.N == 0 {
		return ptchan.Thread{}, fmt.Errorf("gateway context response exceeds %d bytes", maxContextResponseBytes)
	}
	return thread.toPtchan(), nil
}

type contextThread struct {
	Board string        `json:"board"`
	ID    int64         `json:"thread_id"`
	Posts []contextPost `json:"posts"`
}

type contextPost struct {
	Board      string       `json:"board"`
	ThreadID   int64        `json:"thread_id"`
	PostID     int64        `json:"post_id"`
	Date       time.Time    `json:"date"`
	Subject    string       `json:"subject"`
	Message    string       `json:"message"`
	Name       string       `json:"name"`
	Tripcode   string       `json:"tripcode"`
	Capcode    string       `json:"capcode"`
	References []contextRef `json:"references"`
}

type contextRef struct {
	ThreadID int64 `json:"thread_id"`
	PostID   int64 `json:"post_id"`
}

func (t contextThread) toPtchan() ptchan.Thread {
	thread := ptchan.Thread{Board: t.Board, PostID: t.ID}
	for _, post := range t.Posts {
		if post.PostID == t.ID {
			thread.Date = post.Date
			thread.Board = post.Board
			thread.Name = post.Name
			thread.Subject = post.Subject
			thread.Message = post.Message
			thread.PostID = post.PostID
			thread.Tripcode = post.Tripcode
			thread.Capcode = post.Capcode
			thread.Quotes = refsToQuotes(post.References)
			continue
		}
		thread.Replies = append(thread.Replies, ptchan.Post{
			Date:     post.Date,
			Board:    post.Board,
			Name:     post.Name,
			Message:  post.Message,
			ThreadID: post.ThreadID,
			PostID:   post.PostID,
			Tripcode: post.Tripcode,
			Capcode:  post.Capcode,
			Quotes:   refsToQuotes(post.References),
		})
	}
	return thread
}

func refsToQuotes(refs []contextRef) []ptchan.Quote {
	quotes := make([]ptchan.Quote, 0, len(refs))
	for _, ref := range refs {
		quotes = append(quotes, ptchan.Quote{ThreadID: ref.ThreadID, PostID: ref.PostID})
	}
	return quotes
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
