package server

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// queryFilter drops the tracking parameters of cache.ignore_query from a query string. The names
// are in lower case, and one ending in * is a prefix.
type queryFilter struct {
	exact    map[string]bool
	prefixes []string
}

func newQueryFilter(names []string) queryFilter {
	f := queryFilter{exact: map[string]bool{}}
	for _, name := range names {
		name = strings.ToLower(name)
		if prefix, ok := strings.CutSuffix(name, "*"); ok {
			f.prefixes = append(f.prefixes, prefix)
		} else {
			f.exact[name] = true
		}
	}
	return f
}

// ignores reports whether the parameter name is one to drop.
func (f queryFilter) ignores(name string) bool {
	name = strings.ToLower(name)
	return f.exact[name] || slices.ContainsFunc(f.prefixes, func(p string) bool { return strings.HasPrefix(name, p) })
}

// clean returns the query string raw without the ignored parameters, except those in keep (the
// parameters a Load of the page reads), with the rest sorted by name. Parameters of one name keep
// their order, and each keeps its spelling, so the page reads what the visitor sent.
func (f queryFilter) clean(raw string, keep []string) string {
	type param struct{ name, text string }
	var kept []param
	for _, text := range strings.Split(raw, "&") {
		if text == "" {
			continue
		}
		key, _, _ := strings.Cut(text, "=")
		name, err := url.QueryUnescape(key)
		if err != nil {
			name = key
		}
		if f.ignores(name) && !slices.Contains(keep, name) {
			continue
		}
		kept = append(kept, param{name, text})
	}
	slices.SortStableFunc(kept, func(a, b param) int { return strings.Compare(a.name, b.name) })
	texts := make([]string, len(kept))
	for i, p := range kept {
		texts[i] = p.text
	}
	return strings.Join(texts, "&")
}

// pageURI is the request URI the whole-page cache keys by and renders for: the path as the visitor
// wrote it, and the query cleaned by queryFilter.clean.
func (s *server) pageURI(r *http.Request, pattern string) string {
	u := url.URL{Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: s.ignore.clean(r.URL.RawQuery, s.pageQuery[pattern])}
	return u.RequestURI()
}

// pageKeyPrefix starts every key of the whole-page cache, which the keys of the render cache, a
// hash in hex, never do.
const pageKeyPrefix = "page\x00"

// pageKey is the key of the entry of the whole-page cache for the request. It holds what the page
// can depend on without looking at the visitor: the scheme, the Host header (never
// X-Forwarded-Host), the path and the cleaned query. The build and the SSR bundle come first,
// since the page is made by them. NUL bytes part the fields, and none of them holds one: an
// escaped path writes a NUL as %00, and Go refuses a Host with one. pagePath reads the path back.
func (s *server) pageKey(r *http.Request, pattern string, f *front) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return pageKeyPrefix + f.build.ID + "\x00" + f.build.bundle + "\x00" + scheme + "://" + strings.ToLower(r.Host) + "\x00" + s.pageURI(r, pattern)
}

// pagePath returns the decoded path of a key of the whole-page cache, as r.URL.Path has it, and
// false for a key of the render cache.
func pagePath(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, pageKeyPrefix)
	if !ok {
		return "", false
	}
	fields := strings.SplitN(rest, "\x00", 4)
	if len(fields) != 4 {
		return "", false
	}
	// The request URI is the escaped path, which writes ? as %3F, then the query.
	escaped, _, _ := strings.Cut(fields[3], "?")
	path, err := url.PathUnescape(escaped)
	return path, err == nil
}
