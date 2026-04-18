// Package httpx wraps net/http with a User-Agent and retry on 5xx/429
// (honoring Retry-After). The client has no Timeout — use context.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultMaxAttempts = 3
	defaultBackoff     = 300 * time.Millisecond
)

var errRequestFailed = errors.New("request failed")

// Client adds a User-Agent and bounded retry on 5xx, 429, and network errors.
// Safe for concurrent use.
type Client struct {
	http        *http.Client
	userAgent   string
	maxAttempts int
	backoff     time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithMaxAttempts overrides the total attempt budget (3). Values less than 1
// still perform a single request.
func WithMaxAttempts(n int) Option { return func(c *Client) { c.maxAttempts = n } }

// WithBackoff overrides the initial retry delay (300ms). Backoff doubles
// between attempts unless the server sends Retry-After. Values less than or
// equal to zero fall back to the default.
func WithBackoff(d time.Duration) Option { return func(c *Client) { c.backoff = d } }

// New returns a Client with the given User-Agent and optional overrides.
func New(userAgent string, opts ...Option) *Client {
	c := &Client{
		http:        &http.Client{},
		userAgent:   userAgent,
		maxAttempts: defaultMaxAttempts,
		backoff:     defaultBackoff,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.maxAttempts < 1 {
		c.maxAttempts = 1
	}

	if c.backoff <= 0 {
		c.backoff = defaultBackoff
	}

	return c
}

// Do sends req, cloning it before adding headers and retrying transient
// failures with exponential backoff or the server's Retry-After. Callers must
// close Response.Body.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	if c.userAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	delay := c.backoff

	var lastErr error

	// Network errors (err != nil) always retry; status-code retry is limited
	// to the 5xx/429 set defined by shouldRetryStatus.
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		resp, err := c.http.Do(req)
		if err == nil && !shouldRetryStatus(resp.StatusCode) {
			return resp, nil
		}

		lastErr = drainAndClassify(resp, err)

		if attempt+1 >= c.maxAttempts {
			break
		}

		select {
		case <-req.Context().Done():
			return nil, fmt.Errorf("http: %w", req.Context().Err())
		case <-time.After(backoffFor(resp, delay)):
		}

		delay *= 2
	}

	if lastErr == nil {
		lastErr = errRequestFailed
	}

	return nil, fmt.Errorf("http: %w", lastErr)
}

// GetJSON GETs rawURL, expects a 2xx response, and decodes the body into dest.
func (c *Client) GetJSON(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}

	return nil
}

func shouldRetryStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func drainAndClassify(resp *http.Response, err error) error {
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	if err != nil {
		return err
	}

	if resp != nil {
		return fmt.Errorf("http %d", resp.StatusCode)
	}

	return nil
}

// backoffFor returns Retry-After (seconds or HTTP-date) when the server
// provides one, otherwise the fallback.
func backoffFor(resp *http.Response, fallback time.Duration) time.Duration {
	if resp == nil {
		return fallback
	}

	v := resp.Header.Get("Retry-After")
	if v == "" {
		return fallback
	}

	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}

	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}

	return fallback
}
