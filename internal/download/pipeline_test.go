package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panakour/openpix/internal/httpx"
	"github.com/panakour/openpix/internal/provider"
)

// imgFor builds a deterministic test image pointing at the given server path.
func imgFor(srv *httptest.Server, id, slug string) provider.Image {
	return provider.Image{
		ID:     id,
		URL:    srv.URL + "/" + slug + ".jpg",
		Title:  slug,
		Source: "test",
		Mime:   "image/jpeg",
	}
}

// imgPayload returns the bytes the test server serves for an image — small
// distinguishable per-id pattern so a wrong file is easy to spot.
func imgPayload(id string) []byte {
	return []byte("OPENPIX_TEST_" + id + "_BYTES")
}

func newImageServer(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Encode the requested basename as the image id so payload matches lookups.
		base := filepath.Base(r.URL.Path)
		id := strings.TrimSuffix(base, filepath.Ext(base))

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "")
		_, _ = w.Write(imgPayload(id))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func newPipeline(t *testing.T, dir string, onEvent func(Event)) *Pipeline {
	t.Helper()

	return &Pipeline{
		HTTP:        httpx.New("test", httpx.WithBackoff(time.Millisecond)),
		OutDir:      dir,
		Concurrency: 4,
		OnEvent:     onEvent,
	}
}

func TestPipeline_DownloadsAllImages(t *testing.T) {
	t.Parallel()

	srv := newImageServer(t)
	dir := t.TempDir()

	var done atomic.Int32

	p := newPipeline(t, dir, func(e Event) {
		if e.Status == StatusDone {
			done.Add(1)
		}
	})

	images := []provider.Image{
		imgFor(srv, "a", "alpha"),
		imgFor(srv, "b", "beta"),
		imgFor(srv, "c", "gamma"),
	}

	if err := p.Run(context.Background(), images); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := done.Load(); got != 3 {
		t.Errorf("StatusDone events = %d; want 3", got)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("dir entries = %d; want 3", len(entries))
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".openpix-") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

func TestPipeline_AtomicWritesOnFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	p := newPipeline(t, dir, nil)

	images := []provider.Image{imgFor(srv, "a", "alpha")}

	if err := p.Run(context.Background(), images); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".openpix-") {
			t.Errorf("temp file leaked on failure: %s", e.Name())
		}
	}

	// No final image should be present either.
	if len(entries) != 0 {
		t.Errorf("dir entries = %d; want 0 (failed download leaves nothing behind)", len(entries))
	}
}

func TestPipeline_RequiresHTTPClient(t *testing.T) {
	t.Parallel()

	p := &Pipeline{OutDir: t.TempDir()}
	err := p.Run(context.Background(), []provider.Image{{URL: "https://example.com/image.jpg"}})
	if err == nil {
		t.Fatal("Run: want error for missing HTTP client")
	}
}

func TestPipeline_IdempotentRerun(t *testing.T) {
	t.Parallel()

	srv := newImageServer(t)
	dir := t.TempDir()

	var (
		hits     atomic.Int32
		statuses []Status
		mu       sync.Mutex
	)

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		base := filepath.Base(r.URL.Path)
		id := strings.TrimSuffix(base, filepath.Ext(base))
		_, _ = w.Write(imgPayload(id))
	})

	p := newPipeline(t, dir, func(e Event) {
		mu.Lock()
		statuses = append(statuses, e.Status)
		mu.Unlock()
	})

	images := []provider.Image{imgFor(srv, "a", "alpha")}

	if err := p.Run(context.Background(), images); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	if hits.Load() != 1 {
		t.Fatalf("first Run: server hits = %d; want 1", hits.Load())
	}

	// Second run should skip — file is on disk already.
	statuses = nil

	if err := p.Run(context.Background(), images); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if hits.Load() != 1 {
		t.Errorf("second Run hit server again: hits = %d", hits.Load())
	}

	if len(statuses) != 1 || statuses[0] != StatusSkipped {
		t.Errorf("second Run statuses = %v; want [StatusSkipped]", statuses)
	}
}

func TestPipeline_PerImageFailureDoesNotCancelSiblings(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "broken") {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	dir := t.TempDir()

	var counts struct {
		done, failed atomic.Int32
	}

	p := newPipeline(t, dir, func(e Event) {
		switch e.Status {
		case StatusDone:
			counts.done.Add(1)
		case StatusFailed:
			counts.failed.Add(1)
		case StatusSkipped:
		}
	})

	images := []provider.Image{
		imgFor(srv, "1", "good1"),
		imgFor(srv, "2", "broken1"),
		imgFor(srv, "3", "good2"),
		imgFor(srv, "4", "broken2"),
	}

	if err := p.Run(context.Background(), images); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if counts.done.Load() != 2 {
		t.Errorf("done = %d; want 2", counts.done.Load())
	}

	if counts.failed.Load() != 2 {
		t.Errorf("failed = %d; want 2", counts.failed.Load())
	}
}

func TestPipeline_PropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	// Server that sleeps so cancellation has something to interrupt.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}

		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	p := newPipeline(t, dir, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	images := []provider.Image{imgFor(srv, "a", "slow")}

	err := p.Run(ctx, images)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run: err = %v; want context.Canceled", err)
	}
}

func TestPipeline_CreatesOutputDirectory(t *testing.T) {
	t.Parallel()

	srv := newImageServer(t)
	parent := t.TempDir()
	nested := filepath.Join(parent, "deeply", "nested", "out")

	p := newPipeline(t, nested, nil)

	if err := p.Run(context.Background(), []provider.Image{imgFor(srv, "a", "alpha")}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(nested); err != nil {
		t.Errorf("output dir not created: %v", err)
	}
}

func TestPipeline_DefaultConcurrencyApplied(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		HTTP:        httpx.New("test"),
		OutDir:      t.TempDir(),
		Concurrency: 0, // unset
	}

	if err := p.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Run must not mutate the receiver; Concurrency stays at the zero value.
	if p.Concurrency != 0 {
		t.Errorf("Run mutated Concurrency: got %d; want 0", p.Concurrency)
	}
}

func TestPipeline_ExistingDirectoryAtDestinationFails(t *testing.T) {
	t.Parallel()

	srv := newImageServer(t)
	dir := t.TempDir()
	img := imgFor(srv, "a", "alpha")
	dest := filepath.Join(dir, Filename(img))

	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatalf("Mkdir(%q): %v", dest, err)
	}

	var events []Event
	p := newPipeline(t, dir, func(e Event) {
		events = append(events, e)
	})

	if err := p.Run(context.Background(), []provider.Image{img}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("events = %d; want 1", len(events))
	}

	if events[0].Status != StatusFailed {
		t.Fatalf("status = %v; want StatusFailed", events[0].Status)
	}
}
