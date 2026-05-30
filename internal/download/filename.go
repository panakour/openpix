package download

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/panakour/openpix/internal/provider"
)

const (
	maxSlugLen = 50
	hashLen    = 8
)

var slugNonWord = regexp.MustCompile(`[^\w\-]+`)

// Filename builds <source>-<sha256(URL)[:8]>-<slug>.<ext> — deterministic so
// reruns skip what's already on disk.
func Filename(img provider.Image) string {
	return fmt.Sprintf("%s-%s-%s.%s",
		cmp.Or(img.Source, "image"),
		shortHash(img.URL),
		slug(img.Title, img.ID),
		extensionFor(img),
	)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])[:hashLen]
}

func slug(title, fallback string) string {
	base := cmp.Or(title, fallback)

	base = strings.TrimPrefix(base, "File:")

	if ext := path.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}

	cleaned := strings.Trim(slugNonWord.ReplaceAllString(base, "_"), "_")

	if len(cleaned) > maxSlugLen {
		cleaned = strings.TrimRight(cleaned[:maxSlugLen], "_")
	}

	return cmp.Or(cleaned, "image")
}

func extensionFor(img provider.Image) string {
	return cmp.Or(extFromURL(img.URL), extFromMime(img.Mime), "jpg")
}

func extFromURL(rawURL string) string {
	if i := strings.IndexAny(rawURL, "?#"); i >= 0 {
		rawURL = rawURL[:i]
	}

	ext := strings.ToLower(strings.TrimPrefix(path.Ext(rawURL), "."))

	switch ext {
	case "jpg", "jpeg", "png", "webp", "gif":
		return ext
	}

	return ""
}

func extFromMime(mime string) string {
	switch mime {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	}

	return ""
}
