package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_DoSetsUserAgent(t *testing.T) {
	t.Parallel()

	var got string

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
	}))
	defer srv.Close()

	c := New("openpix/test")
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	_ = resp.Body.Close()

	if got != "openpix/test" {
		t.Errorf("UA = %q; want openpix/test", got)
	}
}

func TestClient_DoDoesNotMutateCallerRequest(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := New("openpix/test").Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	if got := req.Header.Get("User-Agent"); got != "" {
		t.Errorf("request header mutated: User-Agent = %q; want empty", got)
	}
}

func TestClient_DoRetriesOn5xx(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("test", WithBackoff(time.Millisecond))

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d; want 2 (one 502, one retry)", got)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}
}

func TestClient_DoRetriesOn429WithRetryAfter(t *testing.T) {
	t.Parallel()

	var (
		calls atomic.Int32
		first time.Time
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			first = time.Now()
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("test", WithBackoff(time.Millisecond)) // would normally backoff 1ms, Retry-After should override

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	elapsed := time.Since(first)

	if elapsed < 900*time.Millisecond {
		t.Errorf("Retry-After ignored: elapsed %v, want ≥ 1s", elapsed)
	}
}

func TestClient_DoFailsAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New("test", WithBackoff(time.Millisecond))

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Do: want error after exhausting retries, got nil")
	}

	if got := calls.Load(); got != int32(c.maxAttempts) {
		t.Errorf("calls = %d; want %d", got, c.maxAttempts)
	}
}

func TestClient_DoStillAttemptsOnceWhenMaxAttemptsIsZero(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New("test", WithMaxAttempts(0), WithBackoff(time.Millisecond))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Do: want error after single failed attempt, got nil")
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d; want 1", got)
	}
}

func TestClient_DoDoesNotRetry4xx(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New("test")
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: 4xx should return without error, got %v", err)
	}
	_ = resp.Body.Close()

	if calls.Load() != 1 {
		t.Errorf("calls = %d; 4xx should not be retried", calls.Load())
	}
}

func TestClient_DoCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New("test", WithBackoff(50*time.Millisecond), WithMaxAttempts(5))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	resp, err := c.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

func TestClient_GetJSONDecodes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"test","count":42}`))
	}))
	defer srv.Close()

	var got struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	if err := New("test").GetJSON(context.Background(), srv.URL, &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}

	if got.Name != "test" || got.Count != 42 {
		t.Errorf("decoded = %+v; want {test 42}", got)
	}
}

func TestClient_GetJSONReturnsHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	var dest map[string]any

	err := New("test").GetJSON(context.Background(), srv.URL, &dest)
	if err == nil {
		t.Fatal("GetJSON: want error on 400, got nil")
	}
}

func TestNew_DefaultsNonPositiveBackoff(t *testing.T) {
	t.Parallel()

	c := New("test", WithBackoff(0))
	if c.backoff != defaultBackoff {
		t.Errorf("backoff = %v; want %v", c.backoff, defaultBackoff)
	}
}

func TestBackoffFor_HonorsIntegerSeconds(t *testing.T) {
	t.Parallel()

	resp := &http.Response{Header: http.Header{"Retry-After": {"3"}}}

	if got, want := backoffFor(resp, time.Hour), 3*time.Second; got != want {
		t.Errorf("backoffFor = %v; want %v", got, want)
	}
}

func TestBackoffFor_FallsBackOnGarbage(t *testing.T) {
	t.Parallel()

	cases := []*http.Response{
		nil,
		{Header: http.Header{}},
		{Header: http.Header{"Retry-After": {"not-a-number"}}},
	}

	for i, resp := range cases {
		if got := backoffFor(resp, 42*time.Millisecond); got != 42*time.Millisecond {
			t.Errorf("case %d: got %v; want fallback 42ms", i, got)
		}
	}
}

func TestBackoffFor_ParsesHTTPDate(t *testing.T) {
	t.Parallel()

	when := time.Now().Add(2 * time.Second)
	resp := &http.Response{Header: http.Header{"Retry-After": {when.UTC().Format(http.TimeFormat)}}}

	got := backoffFor(resp, time.Hour)
	// HTTP-date has 1-second resolution; allow ±1s slack.
	if got < time.Second || got > 3*time.Second {
		t.Errorf("backoffFor = %v; want ~2s", got)
	}
}

func TestShouldRetryStatus(t *testing.T) {
	t.Parallel()

	cases := map[int]bool{
		200: false,
		301: false,
		400: false,
		404: false,
		429: true,
		500: true,
		502: true,
		503: true,
		599: true,
	}

	for status, want := range cases {
		if got := shouldRetryStatus(status); got != want {
			t.Errorf("shouldRetryStatus(%d) = %v; want %v", status, got, want)
		}
	}
}

// Sanity check that retry-after upper bounds are reasonable. If a server sends
// "Retry-After: 999999" we'd otherwise sleep for ages — the test exists to
// document behavior, not assert a specific cap.
func TestBackoffFor_LargeValuesPassThrough(t *testing.T) {
	t.Parallel()

	resp := &http.Response{Header: http.Header{"Retry-After": {strconv.Itoa(999999)}}}
	got := backoffFor(resp, time.Second)

	if got != 999999*time.Second {
		t.Errorf("backoffFor large = %v; want %v", got, 999999*time.Second)
	}
}
