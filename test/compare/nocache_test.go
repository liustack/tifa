package compare

import (
	"net/http"
	"slices"
	"testing"
)

// An answer that a cache may keep says so in its headers. The Node apps must send none of them, so
// that every request of the comparison does the work again in the three of them.
func TestCacheSignsFindsWhatLetsAnAnswerBeKept(t *testing.T) {
	for _, tc := range []struct {
		header http.Header
		want   []string
	}{
		{http.Header{}, nil},
		{http.Header{"Cache-Control": {"private, no-cache, no-store, max-age=0, must-revalidate"}}, nil},
		{http.Header{"Vary": {"Accept-Encoding"}}, nil},
		{http.Header{"Etag": {`"abc"`}}, []string{"ETag"}},
		{http.Header{"Last-Modified": {"Mon, 09 Nov 2026 00:00:00 GMT"}}, []string{"Last-Modified"}},
		{http.Header{"Cache-Control": {"public, max-age=60"}}, []string{"Cache-Control max-age=60"}},
		{http.Header{"Cache-Control": {"s-maxage=31536000, stale-while-revalidate"}}, []string{"Cache-Control s-maxage=31536000"}},
		{http.Header{"X-Nextjs-Cache": {"HIT"}}, []string{"x-nextjs-cache"}},
		{http.Header{"X-Nextjs-Prerender": {"1"}, "Etag": {"x"}}, []string{"ETag", "x-nextjs-prerender"}},
	} {
		if got := CacheSigns(tc.header); !slices.Equal(got, tc.want) {
			t.Errorf("%v: %q, want %q", tc.header, got, tc.want)
		}
	}
}
