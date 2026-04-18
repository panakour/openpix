package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/panakour/openpix/internal/httpx"
)

const openverseFixture = `{
  "result_count": 3,
  "results": [
    {
      "id": "aaa",
      "title": "Forest at Dawn",
      "url": "https://live.static/example/a.jpg",
      "license": "by-sa",
      "creator": "Ansel Test",
      "width": 4000, "height": 3000
    },
    {
      "id": "bbb",
      "title": "Small Thumbnail",
      "url": "https://live.static/example/b.jpg",
      "license": "cc0",
      "creator": "Anon",
      "width": 640, "height": 480
    },
    {
      "id": "ccc",
      "title": "Placeholder",
      "url": "",
      "license": "cc0",
      "width": 3000, "height": 2000
    }
  ]
}`

func TestOpenverseSearch_QueryAndLicense(t *testing.T) {
	t.Parallel()

	var captured map[string][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openverseFixture))
	}))
	defer srv.Close()

	o := &Openverse{client: httpx.New("test"), base: srv.URL}

	images, err := o.Search(context.Background(), Query{Term: "forest", Count: 2, License: "cc0,by-sa"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got := captured["q"]; len(got) != 1 || got[0] != "forest" {
		t.Errorf("q = %v; want [forest]", got)
	}

	if got := captured["license"]; len(got) != 1 || got[0] != "cc0,by-sa" {
		t.Errorf("license = %v; want [cc0,by-sa]", got)
	}

	if len(images) != 2 {
		t.Fatalf("len(images) = %d; want 2 (Count cap)", len(images))
	}

	if images[0].Title != "Forest at Dawn" {
		t.Errorf("images[0].Title = %q; want 'Forest at Dawn'", images[0].Title)
	}

	if images[0].Source != SourceOpenverse {
		t.Errorf("images[0].Source = %q; want %q", images[0].Source, SourceOpenverse)
	}
}

func TestOpenverseSearch_PassesSizeThrough(t *testing.T) {
	t.Parallel()

	var captured map[string][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openverseFixture))
	}))
	defer srv.Close()

	o := &Openverse{client: httpx.New("test"), base: srv.URL}

	if _, err := o.Search(context.Background(), Query{Term: "x", Count: 1, SizeBucket: "small"}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got := captured["size"]; len(got) != 1 || got[0] != "small" {
		t.Errorf("size = %v; want [small]", got)
	}
}

func TestOpenverseSearch_RequestsLargeWhenSizeFiltered(t *testing.T) {
	t.Parallel()

	var captured map[string][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openverseFixture))
	}))
	defer srv.Close()

	o := &Openverse{client: httpx.New("test"), base: srv.URL}

	if _, err := o.Search(context.Background(), Query{Term: "x", Count: 1, MinWidth: 1280}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got := captured["size"]; len(got) != 1 || got[0] != "large" {
		t.Errorf("size = %v; want [large] when MinWidth is set", got)
	}

	captured = nil

	if _, err := o.Search(context.Background(), Query{Term: "x", Count: 1}); err != nil {
		t.Fatalf("Search (no filter): %v", err)
	}

	if got := captured["size"]; len(got) != 0 {
		t.Errorf("size = %v; want unset when no min filter", got)
	}
}

func TestOpenverseSearch_MinWidthFilter(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openverseFixture))
	}))
	defer srv.Close()

	o := &Openverse{client: httpx.New("test"), base: srv.URL}

	images, err := o.Search(context.Background(), Query{Term: "x", Count: 10, MinWidth: 1280})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Only the 4000×3000 entry passes: small thumb filtered out, blank URL skipped.
	if len(images) != 1 {
		t.Fatalf("len(images) = %d; want 1", len(images))
	}
}

func TestOpenverseSearch_SkipsBlankURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openverseFixture))
	}))
	defer srv.Close()

	o := &Openverse{client: httpx.New("test"), base: srv.URL}

	images, err := o.Search(context.Background(), Query{Term: "x", Count: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	for _, img := range images {
		if img.URL == "" {
			t.Errorf("blank URL leaked through: %+v", img)
		}
	}
}
