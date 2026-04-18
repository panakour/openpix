package provider

import (
	"context"
	"fmt"
	"html"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/panakour/openpix/internal/httpx"
)

const (
	apiTimeout       = 30 * time.Second
	wikimediaAPI     = "https://commons.wikimedia.org/w/api.php"
	featuredCategory = "Category:Featured_pictures_on_Wikimedia_Commons"
	wikimediaFileNS  = "6"

	// 500 is the per-request cap for anonymous clients.
	wikimediaPoolSize = 500
	// gsrlimit anonymous range: floor 20 (gives size filter headroom), cap 50.
	wikimediaSearchMin = 20
	wikimediaSearchMax = 50
)

func wikimediaSearchLimit(count int) int {
	return min(max(count*5, wikimediaSearchMin), wikimediaSearchMax)
}

// Wikimedia searches Wikimedia Commons.
type Wikimedia struct {
	client *httpx.Client
	base   string // overridable in tests
}

// NewWikimedia returns a Wikimedia provider that uses client.
func NewWikimedia(client *httpx.Client) *Wikimedia {
	return &Wikimedia{client: client, base: wikimediaAPI}
}

// Name implements Provider.
func (*Wikimedia) Name() string { return SourceWikimedia }

// Search implements Provider.
func (w *Wikimedia) Search(ctx context.Context, q Query) ([]Image, error) {
	if err := q.Validate(); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	var pages []wikimediaPage
	var err error

	if q.Term == "" {
		pages, err = w.featuredPool(ctx)
	} else {
		pages, err = w.fullText(ctx, q.Term, wikimediaSearchLimit(q.Count))
	}

	if err != nil {
		return nil, err
	}

	images := make([]Image, 0, len(pages))
	for _, p := range pages {
		img, ok := p.toImage()
		if !ok {
			continue
		}

		if !passesSizeFilter(q, img.Width, img.Height) {
			continue
		}

		if !fitsSizeBucket(q.SizeBucket, img.Width) {
			continue
		}

		images = append(images, img)
	}

	if q.Term == "" {
		rand.Shuffle(len(images), func(i, j int) { images[i], images[j] = images[j], images[i] })
	}

	if len(images) > q.Count {
		images = images[:q.Count]
	}

	return images, nil
}

func (w *Wikimedia) featuredPool(ctx context.Context) ([]wikimediaPage, error) {
	// Random sort-key letter → different 500-image slice each run.
	start := string(rune('A' + rand.IntN(26)))

	return w.queryPages(ctx, url.Values{
		"action":              {"query"},
		"format":              {"json"},
		"formatversion":       {"2"},
		"generator":           {"categorymembers"},
		"gcmtitle":            {featuredCategory},
		"gcmtype":             {"file"},
		"gcmlimit":            {strconv.Itoa(wikimediaPoolSize)},
		"gcmstartsortkey":     {start},
		"prop":                {"imageinfo"},
		"iiprop":              {"url|size|mime|extmetadata"},
		"iiextmetadatafilter": {"LicenseShortName|Artist"},
	})
}

func (w *Wikimedia) fullText(ctx context.Context, term string, limit int) ([]wikimediaPage, error) {
	return w.queryPages(ctx, url.Values{
		"action":              {"query"},
		"format":              {"json"},
		"formatversion":       {"2"},
		"generator":           {"search"},
		"gsrsearch":           {term + " filetype:bitmap"}, // excludes SVG/PDF server-side
		"gsrnamespace":        {wikimediaFileNS},
		"gsrlimit":            {strconv.Itoa(limit)},
		"prop":                {"imageinfo"},
		"iiprop":              {"url|size|mime|extmetadata"},
		"iiextmetadatafilter": {"LicenseShortName|Artist"},
	})
}

func (w *Wikimedia) queryPages(ctx context.Context, params url.Values) ([]wikimediaPage, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	var resp wikimediaResponse

	if err := w.client.GetJSON(ctx, w.base+"?"+params.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("wikimedia api: %w", err)
	}

	return resp.Query.Pages, nil
}

type wikimediaResponse struct {
	Query struct {
		Pages []wikimediaPage `json:"pages"`
	} `json:"query"`
}

type wikimediaPage struct {
	PageID    int                `json:"pageid"`
	Title     string             `json:"title"`
	ImageInfo []wikimediaImgInfo `json:"imageinfo"`
}

type wikimediaImgInfo struct {
	URL     string                      `json:"url"`
	Width   int                         `json:"width"`
	Height  int                         `json:"height"`
	Size    int64                       `json:"size"`
	Mime    string                      `json:"mime"`
	Extmeta map[string]wikimediaExtmeta `json:"extmetadata"`
}

type wikimediaExtmeta struct {
	Value string `json:"value"`
}

func (p wikimediaPage) toImage() (Image, bool) {
	if len(p.ImageInfo) == 0 {
		return Image{}, false
	}

	info := p.ImageInfo[0]

	switch info.Mime {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return Image{}, false
	}

	return Image{
		ID:        strconv.Itoa(p.PageID),
		URL:       info.URL,
		Title:     strings.TrimPrefix(p.Title, "File:"),
		Source:    SourceWikimedia,
		License:   info.Extmeta["LicenseShortName"].Value,
		Creator:   stripHTML(info.Extmeta["Artist"].Value),
		Width:     info.Width,
		Height:    info.Height,
		Mime:      info.Mime,
		SizeBytes: info.Size,
	}, true
}

var htmlTagRE = regexp.MustCompile(`<[^>]+>`)

func stripHTML(s string) string {
	if s == "" {
		return ""
	}

	return strings.TrimSpace(html.UnescapeString(htmlTagRE.ReplaceAllString(s, "")))
}
