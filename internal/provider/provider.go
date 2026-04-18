// Package provider defines query and image types plus the built-in providers.
package provider

import (
	"context"
	"errors"
	"fmt"
)

// Image describes one candidate image returned by a Provider.
type Image struct {
	ID        string
	URL       string
	Title     string
	Source    string // SourceWikimedia | SourceOpenverse
	License   string
	Creator   string
	Width     int
	Height    int
	Mime      string
	SizeBytes int64
}

// SourceOpenverse and SourceWikimedia identify the built-in providers.
const (
	SourceOpenverse = "openverse"
	SourceWikimedia = "wikimedia"
)

// Query describes the caller's desired result set.
type Query struct {
	Term       string // empty = curated/random per provider
	Count      int
	MinWidth   int
	MinHeight  int
	License    string // Openverse-only
	SizeBucket string // "", "small", "medium", "large" — width buckets: ≤640, 640-1600, >1600
}

// Validate reports whether q is internally consistent.
func (q Query) Validate() error {
	switch {
	case q.Count <= 0:
		return errors.New("count must be greater than 0")
	case q.MinWidth < 0:
		return errors.New("min width must be 0 or greater")
	case q.MinHeight < 0:
		return errors.New("min height must be 0 or greater")
	case !isValidSizeBucket(q.SizeBucket):
		return fmt.Errorf("size %q: want one of small, medium, large", q.SizeBucket)
	default:
		return nil
	}
}

func isValidSizeBucket(bucket string) bool {
	switch bucket {
	case "", "small", "medium", "large":
		return true
	default:
		return false
	}
}

// fitsSizeBucket reports whether width matches the requested bucket.
// Width 0 (unknown) passes an otherwise valid bucket.
func fitsSizeBucket(bucket string, width int) bool {
	switch bucket {
	case "":
		return true
	case "small":
		return width == 0 || width <= 640
	case "medium":
		return width == 0 || (width > 640 && width <= 1600)
	case "large":
		return width == 0 || width > 1600
	default:
		return false
	}
}

// Provider searches a specific image source.
type Provider interface {
	// Name returns the short source name used in UI and filenames.
	Name() string
	// Search returns images matching q, up to q.Count.
	Search(ctx context.Context, q Query) ([]Image, error)
}

// passesSizeFilter treats Width/Height == 0 as unknown (passes, doesn't fail).
func passesSizeFilter(q Query, width, height int) bool {
	if q.MinWidth > 0 && width > 0 && width < q.MinWidth {
		return false
	}

	if q.MinHeight > 0 && height > 0 && height < q.MinHeight {
		return false
	}

	return true
}
