// Package download fetches images concurrently and writes them atomically.
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/panakour/openpix/internal/httpx"
	"github.com/panakour/openpix/internal/provider"
	"golang.org/x/sync/errgroup"
)

// Status reports the outcome of processing a single image.
type Status int

const (
	// StatusDone means the image was downloaded and written successfully.
	StatusDone Status = iota
	// StatusSkipped means a non-empty destination file already existed.
	StatusSkipped
	// StatusFailed means the image could not be downloaded or written.
	StatusFailed
)

// DefaultConcurrency is the worker limit used when Pipeline.Concurrency is unset.
const DefaultConcurrency = 4

// Event describes the result of processing one image.
type Event struct {
	Image  provider.Image
	Status Status
	Path   string
	Bytes  int64
	Err    error
}

// Pipeline fetches images and writes each via tempfile + os.Rename. Per-image
// failures are reported through OnEvent; only ctx cancellation aborts the batch.
type Pipeline struct {
	HTTP        *httpx.Client
	OutDir      string
	Concurrency int
	OnEvent     func(Event)
}

// Run downloads images into OutDir, creating it if needed.
func (p *Pipeline) Run(ctx context.Context, images []provider.Image) error {
	if p.HTTP == nil {
		return errors.New("missing HTTP client")
	}

	if err := os.MkdirAll(p.OutDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	n := DefaultConcurrency
	if p.Concurrency > 0 {
		n = p.Concurrency
	}

	grp, gctx := errgroup.WithContext(ctx)
	grp.SetLimit(n)

	for _, img := range images {
		grp.Go(func() error {
			evt := p.fetchOne(gctx, img)
			if p.OnEvent != nil {
				p.OnEvent(evt)
			}

			if errors.Is(evt.Err, context.Canceled) {
				return evt.Err
			}

			return nil
		})
	}

	if err := grp.Wait(); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	return nil
}

func (p *Pipeline) fetchOne(ctx context.Context, img provider.Image) Event {
	dest := filepath.Join(p.OutDir, Filename(img))

	info, err := os.Stat(dest)
	switch {
	case err == nil && !info.Mode().IsRegular():
		return Event{
			Image:  img,
			Status: StatusFailed,
			Path:   dest,
			Err:    fmt.Errorf("destination exists and is not a regular file"),
		}
	case err == nil && info.Size() > 0:
		return Event{Image: img, Status: StatusSkipped, Path: dest, Bytes: info.Size()}
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return Event{Image: img, Status: StatusFailed, Path: dest, Err: fmt.Errorf("stat destination: %w", err)}
	}

	written, err := p.streamTo(ctx, img.URL, dest)
	if err != nil {
		return Event{Image: img, Status: StatusFailed, Path: dest, Bytes: written, Err: err}
	}

	return Event{Image: img, Status: StatusDone, Path: dest, Bytes: written}
}

func (p *Pipeline) streamTo(ctx context.Context, url, dest string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}

	resp, err := p.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		return 0, fmt.Errorf("fetch %s: http %d", url, resp.StatusCode)
	}

	tmp, err := os.CreateTemp(p.OutDir, ".openpix-*")
	if err != nil {
		return 0, fmt.Errorf("create temp: %w", err)
	}

	tmpName := tmp.Name()

	written, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()

	if copyErr != nil {
		_ = os.Remove(tmpName)

		return written, fmt.Errorf("write: %w", copyErr)
	}

	if closeErr != nil {
		_ = os.Remove(tmpName)

		return written, fmt.Errorf("close temp: %w", closeErr)
	}

	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)

		return written, fmt.Errorf("rename: %w", err)
	}

	return written, nil
}
