package download

import (
	"strings"
	"testing"

	"github.com/panakour/openpix/internal/provider"
)

func TestFilename(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		img        provider.Image
		wantExt    string
		wantSlug   string
		wantSource string
	}{
		"wikimedia jpeg with spaces": {
			img: provider.Image{
				Source: provider.SourceWikimedia,
				ID:     "12345",
				URL:    "https://upload.wikimedia.org/wikipedia/commons/a/b/Mount_Everest_northeast_ridge.jpg",
				Title:  "Mount Everest northeast ridge",
				Mime:   "image/jpeg",
			},
			wantExt:    "jpg",
			wantSlug:   "Mount_Everest_northeast_ridge",
			wantSource: provider.SourceWikimedia,
		},
		"openverse png no title uses id": {
			img: provider.Image{
				Source: provider.SourceOpenverse,
				ID:     "e945bba3-4817-468f-8740-57d326de5b6d",
				URL:    "https://live.staticflickr.com/foo.png",
				Mime:   "image/png",
			},
			wantExt:    "png",
			wantSlug:   "e945bba3-4817-468f-8740-57d326de5b6d",
			wantSource: provider.SourceOpenverse,
		},
		"long title is truncated": {
			img: provider.Image{
				Source: provider.SourceWikimedia,
				URL:    "https://x/a.jpg",
				Title:  strings.Repeat("abcdefghij", 10),
			},
			wantExt: "jpg",
		},
		"mime fallback when url has no ext": {
			img: provider.Image{
				Source: provider.SourceWikimedia,
				URL:    "https://api.example.com/image?id=42",
				Mime:   "image/webp",
				Title:  "foo",
			},
			wantExt: "webp",
		},
		"strips File: prefix and extension from title": {
			img: provider.Image{
				Source: provider.SourceWikimedia,
				URL:    "https://x/a.jpg",
				Title:  "File:Some Photo.jpg",
			},
			wantExt:  "jpg",
			wantSlug: "Some_Photo",
		},
		"default jpg when all else fails": {
			img: provider.Image{
				Source: provider.SourceWikimedia,
				URL:    "https://api.example.com/unknown",
				Title:  "whatever",
			},
			wantExt: "jpg",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := Filename(tc.img)

			if !strings.HasSuffix(got, "."+tc.wantExt) {
				t.Errorf("Filename(%q) = %q; want extension %q", tc.img.Title, got, tc.wantExt)
			}

			if tc.wantSource != "" && !strings.HasPrefix(got, tc.wantSource+"-") {
				t.Errorf("Filename = %q; want source prefix %q", got, tc.wantSource)
			}

			if tc.wantSlug != "" && !strings.Contains(got, tc.wantSlug) {
				t.Errorf("Filename = %q; want slug containing %q", got, tc.wantSlug)
			}

			// Name should not exceed a reasonable upper bound.
			if len(got) > 120 {
				t.Errorf("Filename = %q; too long (%d chars)", got, len(got))
			}
		})
	}
}

func TestFilename_IsStable(t *testing.T) {
	t.Parallel()

	img := provider.Image{
		Source: provider.SourceWikimedia,
		ID:     "1",
		URL:    "https://upload.wikimedia.org/wikipedia/commons/foo.jpg",
		Title:  "Foo bar baz",
	}

	first := Filename(img)
	for range 5 {
		if got := Filename(img); got != first {
			t.Fatalf("Filename not stable: first=%q now=%q", first, got)
		}
	}
}

func TestFilename_DistinctForDifferentURLs(t *testing.T) {
	t.Parallel()

	a := Filename(provider.Image{Source: provider.SourceWikimedia, URL: "https://x/a.jpg", Title: "Same"})
	b := Filename(provider.Image{Source: provider.SourceWikimedia, URL: "https://x/b.jpg", Title: "Same"})

	if a == b {
		t.Errorf("Filename collided for different URLs: %q", a)
	}
}
