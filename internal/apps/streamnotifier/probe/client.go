package probe

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	HTTPClient *http.Client
}

type Channel struct {
	Key      string
	ProbeURL string
	PageURL  string
}

func New() *Client {
	return &Client{
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Client) IsLive(ctx context.Context, channel Channel) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, channel.ProbeURL, nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status: %s", resp.Status)
	}
}
