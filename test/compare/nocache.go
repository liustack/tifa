package compare

import (
	"net/http"
	"strconv"
	"strings"
)

// CacheSigns lists what in the headers of an answer lets a cache keep it or ask for it again cheaply:
// an ETag, a Last-Modified, a max-age or s-maxage above 0, and the headers Next.js sends with a page
// it keeps (x-nextjs-cache) or made at build time (x-nextjs-prerender). It is empty for an answer
// that must be made again for the next request.
func CacheSigns(h http.Header) []string {
	var out []string
	for _, name := range []string{"ETag", "Last-Modified"} {
		if h.Get(name) != "" {
			out = append(out, name)
		}
	}
	for _, d := range strings.Split(h.Get("Cache-Control"), ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(d), "=")
		if !ok || (name != "max-age" && name != "s-maxage") {
			continue
		}
		if n, err := strconv.Atoi(strings.Trim(value, `"`)); err != nil || n > 0 {
			out = append(out, "Cache-Control "+name+"="+value)
		}
	}
	for _, name := range []string{"x-nextjs-cache", "x-nextjs-prerender"} {
		if h.Get(name) != "" {
			out = append(out, name)
		}
	}
	return out
}
