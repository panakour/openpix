package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/panakour/openpix/internal/httpx"
)

const openverseAPI = "https://api.openverse.org"

// Openverse searches the Openverse image API.
type Openverse struct {
	client *httpx.Client
	base   string // overridable in tests
}

// NewOpenverse returns an Openverse provider that uses client.
func NewOpenverse(client *httpx.Client) *Openverse {
	return &Openverse{client: client, base: openverseAPI}
}

// Name implements Provider.
func (*Openverse) Name() string { return SourceOpenverse }

// Search implements Provider.
func (o *Openverse) Search(ctx context.Context, q Query) ([]Image, error) {
	if err := q.Validate(); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	// Anonymous page_size cap is 20; always ask for max so size-filter has headroom.
	params := url.Values{"page_size": {"20"}}

	if q.Term != "" {
		params.Set("q", q.Term)
	}

	if q.License != "" {
		params.Set("license", q.License)
	}

	// Prefer an explicit size bucket; fall back to "large" when a min-dimension
	// filter is set (page 1 is mostly sub-1024 thumbnails for common queries).
	switch {
	case q.SizeBucket != "":
		params.Set("size", q.SizeBucket)
	case q.MinWidth > 0 || q.MinHeight > 0:
		params.Set("size", "large")
	}

	var resp openverseResponse

	if err := o.client.GetJSON(ctx, o.base+"/v1/images/?"+params.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("openverse api: %w", err)
	}

	images := make([]Image, 0, len(resp.Results))

	for _, r := range resp.Results {
		if r.URL == "" {
			continue
		}

		if !passesSizeFilter(q, r.Width, r.Height) {
			continue
		}

		if !fitsSizeBucket(q.SizeBucket, r.Width) {
			continue
		}

		images = append(images, Image{
			ID:      r.ID,
			URL:     r.URL,
			Title:   r.Title,
			Source:  SourceOpenverse,
			License: r.License,
			Creator: r.Creator,
			Width:   r.Width,
			Height:  r.Height,
		})

		if len(images) >= q.Count {
			break
		}
	}

	return images, nil
}

type openverseResponse struct {
	ResultCount int            `json:"result_count"`
	Results     []openverseImg `json:"results"`
}

type openverseImg struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	License string `json:"license"`
	Creator string `json:"creator"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}
