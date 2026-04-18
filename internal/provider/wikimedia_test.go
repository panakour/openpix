package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panakour/openpix/internal/httpx"
)

const wikimediaFixture = `{
  "query": {
    "pages": [
      {
        "pageid": 1,
        "title": "File:Mountain.jpg",
        "imageinfo": [{
          "url": "https://upload.wikimedia.org/commons/1/mountain.jpg",
          "width": 3000, "height": 2000, "size": 1234567,
          "mime": "image/jpeg",
          "extmetadata": {
            "LicenseShortName": {"value": "CC BY-SA 4.0"},
            "Artist": {"value": "<a href='#'>Jane Doe</a>"}
          }
        }]
      },
      {
        "pageid": 2,
        "title": "File:SmallThumbnail.jpg",
        "imageinfo": [{
          "url": "https://upload.wikimedia.org/commons/2/small.jpg",
          "width": 400, "height": 300, "size": 12345,
          "mime": "image/jpeg"
        }]
      },
      {
        "pageid": 3,
        "title": "File:Diagram.svg",
        "imageinfo": [{
          "url": "https://upload.wikimedia.org/commons/3/d.svg",
          "width": 5000, "height": 5000, "size": 9999,
          "mime": "image/svg+xml"
        }]
      }
    ]
  }
}`

func newWikimediaTestServer(t *testing.T, assertParams func(query map[string][]string)) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if assertParams != nil {
			assertParams(r.URL.Query())
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(wikimediaFixture))
	}))
}

func TestWikimediaSearch_FullText(t *testing.T) {
	t.Parallel()

	srv := newWikimediaTestServer(t, func(q map[string][]string) {
		if got := q["generator"]; len(got) != 1 || got[0] != "search" {
			t.Errorf("generator = %v; want [search]", got)
		}

		if got := q["gsrsearch"]; len(got) != 1 || !strings.Contains(got[0], "mountain") {
			t.Errorf("gsrsearch = %v; want substring 'mountain'", got)
		}

		if got := q["gsrsearch"]; len(got) != 1 || !strings.Contains(got[0], "filetype:bitmap") {
			t.Errorf("gsrsearch = %v; want filetype:bitmap", got)
		}
	})
	defer srv.Close()

	w := &Wikimedia{client: httpx.New("test"), base: srv.URL}

	images, err := w.Search(context.Background(), Query{Term: "mountain", Count: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// 3 pages in fixture; SVG filtered out → 2 candidates. With no size filter, both remain.
	if len(images) != 2 {
		t.Fatalf("len(images) = %d; want 2 (SVG filtered)", len(images))
	}

	first := images[0]
	if first.Source != SourceWikimedia {
		t.Errorf("Source = %q; want %q", first.Source, SourceWikimedia)
	}

	if first.Title != "Mountain.jpg" {
		t.Errorf("Title = %q; want 'Mountain.jpg' (File: prefix stripped)", first.Title)
	}

	if first.License != "CC BY-SA 4.0" {
		t.Errorf("License = %q; want 'CC BY-SA 4.0'", first.License)
	}

	if first.Creator != "Jane Doe" {
		t.Errorf("Creator = %q; want 'Jane Doe' (HTML stripped)", first.Creator)
	}
}

func TestWikimediaSearch_MinWidthFilter(t *testing.T) {
	t.Parallel()

	srv := newWikimediaTestServer(t, nil)
	defer srv.Close()

	w := &Wikimedia{client: httpx.New("test"), base: srv.URL}

	images, err := w.Search(context.Background(), Query{Term: "x", Count: 10, MinWidth: 1000})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Only the 3000px image passes MinWidth=1000; small thumb + SVG are excluded.
	if len(images) != 1 {
		t.Fatalf("len(images) = %d; want 1", len(images))
	}

	if images[0].Width < 1000 {
		t.Errorf("image width = %d; filter should have excluded", images[0].Width)
	}
}

func TestWikimediaSearch_SizeBucket(t *testing.T) {
	t.Parallel()

	srv := newWikimediaTestServer(t, nil)
	defer srv.Close()

	w := &Wikimedia{client: httpx.New("test"), base: srv.URL}

	got, err := w.Search(context.Background(), Query{Term: "x", Count: 10, SizeBucket: "small"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Only the 400-wide passes the "small" bucket; 3000-wide is large; SVG filtered.
	if len(got) != 1 || got[0].Width != 400 {
		t.Errorf("Size=small: got %d images (widths: %v); want 1 at 400", len(got), widths(got))
	}

	got, err = w.Search(context.Background(), Query{Term: "x", Count: 10, SizeBucket: "large"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 1 || got[0].Width != 3000 {
		t.Errorf("Size=large: got %d images (widths: %v); want 1 at 3000", len(got), widths(got))
	}
}

func widths(imgs []Image) []int {
	out := make([]int, len(imgs))
	for i, img := range imgs {
		out[i] = img.Width
	}

	return out
}

func TestWikimediaSearch_FeaturedWhenNoTerm(t *testing.T) {
	t.Parallel()

	srv := newWikimediaTestServer(t, func(q map[string][]string) {
		if got := q["generator"]; len(got) != 1 || got[0] != "categorymembers" {
			t.Errorf("generator = %v; want [categorymembers]", got)
		}

		if got := q["gcmtitle"]; len(got) != 1 || got[0] != featuredCategory {
			t.Errorf("gcmtitle = %v; want [%s]", got, featuredCategory)
		}
	})
	defer srv.Close()

	w := &Wikimedia{client: httpx.New("test"), base: srv.URL}

	if _, err := w.Search(context.Background(), Query{Count: 2}); err != nil {
		t.Fatalf("Search: %v", err)
	}
}
