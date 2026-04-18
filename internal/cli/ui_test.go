package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/panakour/openpix/internal/provider"
)

func TestHumanBytes(t *testing.T) {
	t.Parallel()

	cases := map[int64]string{
		0:                  "0 B",
		512:                "512 B",
		1024:               "1.0 KB",
		1500:               "1.5 KB",
		1024 * 1024:        "1.0 MB",
		1024 * 1024 * 1024: "1.0 GB",
	}

	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q; want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in    string
		width int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"longer than ten", 10, "longer th…"},
		{"abc", 1, "…"},
		{"abc", 2, "a…"},
		// CJK chars: each is 2 display columns. width=4 fits one + ellipsis (3 cols).
		{"\u65e5\u672c\u8a9e", 4, "\u65e5\u2026"}, //nolint:gosmopolitan // intentional CJK fixture for width-aware truncation
	}

	for _, c := range cases {
		if got := truncate(c.in, c.width); got != c.want {
			t.Errorf("truncate(%q, %d) = %q; want %q", c.in, c.width, got, c.want)
		}
	}
}

func TestPlural(t *testing.T) {
	t.Parallel()

	if plural(1) != "" {
		t.Errorf(`plural(1) = %q; want ""`, plural(1))
	}

	for _, n := range []int{0, 2, 100} {
		if plural(n) != "s" {
			t.Errorf(`plural(%d) = %q; want "s"`, n, plural(n))
		}
	}
}

func TestSimplifyErr(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`fetch: Get "https://upload.wikimedia.org/foo.jpg": context deadline exceeded`: "timed out",
		`fetch: Get "https://example.com/x": context canceled`:                         "canceled",
		`Get "https://nope.invalid/x": dial tcp: lookup nope.invalid: no such host`:    "dns lookup failed",
		`Get "https://x.com/foo": dial tcp: connection refused`:                        "connection refused",
		`fetch foo: http 503`: "fetch foo: http 503",
	}

	for in, want := range cases {
		got := simplifyErr(errors.New(in))
		if !strings.Contains(got, want) {
			t.Errorf("simplifyErr(%q) = %q; want substring %q", in, got, want)
		}
	}

	if got := simplifyErr(nil); got != "" {
		t.Errorf("simplifyErr(nil) = %q; want empty", got)
	}
}

func TestSimplifyErr_StripsURLNoise(t *testing.T) {
	t.Parallel()

	in := errors.New(`fetch: Get "https://very-long-url.example.com/with/many/path/segments.jpg": some other error`)
	got := simplifyErr(in)

	if strings.Contains(got, "https://very-long-url") {
		t.Errorf("simplifyErr did not strip URL: %q", got)
	}

	if !strings.Contains(got, "some other error") {
		t.Errorf("simplifyErr stripped too much: %q", got)
	}
}

func TestEmptyResultErr(t *testing.T) {
	t.Parallel()

	plain := emptyResultErr(provider.Query{Term: "x"})
	if !strings.Contains(plain.Error(), "no images matched") {
		t.Errorf("plain = %q; want 'no images matched'", plain)
	}

	if strings.Contains(plain.Error(), "min") {
		t.Errorf("plain should not mention min filter: %q", plain)
	}

	withSize := emptyResultErr(provider.Query{Term: "x", MinWidth: 1280, MinHeight: 720})
	if !strings.Contains(withSize.Error(), "1280×720") {
		t.Errorf("withSize = %q; want 1280×720 hint", withSize)
	}

	if !strings.Contains(withSize.Error(), "relaxing the filters") {
		t.Errorf("withSize should suggest relaxing filters: %q", withSize)
	}

	withBucketAndLicense := emptyResultErr(provider.Query{Term: "x", Count: 1, SizeBucket: "large", License: "cc0"})
	if !strings.Contains(withBucketAndLicense.Error(), "size large") {
		t.Errorf("withBucketAndLicense = %q; want size filter", withBucketAndLicense)
	}

	if !strings.Contains(withBucketAndLicense.Error(), "license cc0") {
		t.Errorf("withBucketAndLicense = %q; want license filter", withBucketAndLicense)
	}
}

func TestClampBarWidth(t *testing.T) {
	t.Parallel()

	cases := map[int]int{
		-100:            minBarWidth,
		0:               minBarWidth,
		minBarWidth - 1: minBarWidth,
		minBarWidth:     minBarWidth,
		30:              30,
		maxBarWidth:     maxBarWidth,
		maxBarWidth + 1: maxBarWidth,
		1000:            maxBarWidth,
	}

	for in, want := range cases {
		if got := clampBarWidth(in); got != want {
			t.Errorf("clampBarWidth(%d) = %d; want %d", in, got, want)
		}
	}
}

func TestTotalBytes(t *testing.T) {
	t.Parallel()

	got := totalBytes([]provider.Image{
		{SizeBytes: 100},
		{SizeBytes: 200},
		{SizeBytes: 0}, // unknown — counts as zero
	})

	if got != 300 {
		t.Errorf("totalBytes = %d; want 300", got)
	}
}

func TestModel_FinalErrorPrioritizesSearchError(t *testing.T) {
	t.Parallel()

	searchErr := errors.New("nope")
	m := &model{searchErr: searchErr, runErr: errors.New("ignored"), failed: 5}

	if err := m.finalError(); !errors.Is(err, searchErr) {
		t.Errorf("finalError = %v; want searchErr 'nope' unwrapped", err)
	}
}

func TestModel_FinalErrorReportsFailures(t *testing.T) {
	t.Parallel()

	m := &model{failed: 1}
	if !errors.Is(m.finalError(), errOneOrMoreFailed) {
		t.Errorf("finalError did not return errOneOrMoreFailed")
	}

	clean := &model{}
	if got := clean.finalError(); got != nil {
		t.Errorf("finalError on clean model = %v; want nil", got)
	}
}
