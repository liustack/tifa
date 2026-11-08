package server

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/core"
)

// A page with Cache in its route.go is the same for everyone for a while. Its requests are
// anonymous (see anonymize), so the app cannot make the page depend on who asks, and its HTML
// says that a shared cache in front of the program, such as a CDN, may keep it. The program keeps
// nothing of it itself: its Loads run on every request, and the render memo skips a render whose
// input did not change.

// staleIfError is how long after its time a shared cache may serve a copy when the program fails.
const staleIfError = 24 * time.Hour

// sharedCacheControl is the Cache-Control of a page with Cache for ttl. The browser asks again every
// time (max-age=0) and gets a 304 from the ETag, and a shared cache keeps the page for ttl.
func sharedCacheControl(ttl time.Duration) string {
	return "public, max-age=0, s-maxage=" + strconv.FormatInt(int64(ttl/time.Second), 10) +
		", stale-if-error=" + strconv.FormatInt(int64(staleIfError/time.Second), 10)
}

// setShared gives the answer of a page with Cache the Cache-Control that lets a shared cache keep it,
// unless OnRequest, a Load or a Layout said it is not for everyone: private, no-store or no-cache, or
// a Vary other than Accept-Encoding. Then the headers stay as they are. It notes what it wrote, so a
// failure can take it back (dropShared). Only the GET and HEAD that serve made anonymous are shared (anonymousRequest): any other request
// may have read the visitor.
func setShared(c *tifa.Ctx, ttl time.Duration) {
	a := anonymityOf(c.Request())
	h := c.Writer().Header()
	if a == nil || !headersStorable(h) {
		return
	}
	value := sharedCacheControl(ttl)
	h.Set("Cache-Control", value)
	a.shared = value
}

// dropShared takes back the Cache-Control that setShared wrote, when the answer turns out to be an
// error: a page that failed is not for a shared cache to keep. A Cache-Control the app set is not
// touched.
func dropShared(c *tifa.Ctx) {
	a := anonymityOf(c.Request())
	if a == nil || a.shared == "" {
		return
	}
	h := c.Writer().Header()
	if i := slices.Index(h["Cache-Control"], a.shared); i >= 0 {
		if rest := slices.Delete(slices.Clone(h["Cache-Control"]), i, i+1); len(rest) > 0 {
			h["Cache-Control"] = rest
		} else {
			h.Del("Cache-Control")
		}
	}
	a.shared = ""
}

// sharedRedirect is whether a redirect of a page with Cache may be kept by a shared cache.
func sharedRedirect(status int) bool {
	return status == http.StatusMovedPermanently || status == http.StatusFound || status == http.StatusPermanentRedirect
}

// headersStorable reports whether the headers of an answer let a shared cache keep it: no private,
// no-store or no-cache in Cache-Control, and nothing in Vary but Accept-Encoding, which tifa answers
// to itself. A Cache-Control the framework cannot read is not storable.
func headersStorable(h http.Header) bool {
	for _, value := range h.Values("Cache-Control") {
		directives, ok := splitDirectives(value)
		if !ok {
			return false
		}
		for _, d := range directives {
			name, _, _ := strings.Cut(d, "=")
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "private", "no-store", "no-cache":
				return false
			}
		}
	}
	for _, value := range h.Values("Vary") {
		for _, field := range strings.Split(value, ",") {
			if field = strings.TrimSpace(field); field != "" && !strings.EqualFold(field, "Accept-Encoding") {
				return false
			}
		}
	}
	return true
}

// Stats is tifa.Stats for this server.
func (s *server) Stats() core.Stats {
	var out core.Stats
	out.Memo, out.MemoBytes = s.memo.stats()
	out.MemoSize = s.memo.size
	// Only tifa dev replaces the front end, and with it the pool and its counts.
	if f := s.front.Load(); f != nil && f.pool != nil {
		out.SSR = f.pool.Stats()
	}
	return out
}
