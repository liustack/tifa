package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/core"
	"github.com/liustack/tifa/internal/ssr"
)

// The whole-page cache keeps, for a page with Cache in its route.go, the answer to its anonymous
// GET and HEAD requests (see Server.anonymize), by the cleaned URL (pageKey). An entry holds the
// results of every Load of the page and its HTML, so the HTML and the navigation data of a page
// come from the same moment. While its time lasts a request gets the entry without a Load or a
// render. After that the next request makes a new one, the requests that come meanwhile wait for it,
// and when making it fails the old one stands in for a day.

const (
	// staleIfError is how long after its time an entry may stand in for a page that fails.
	staleIfError = 24 * time.Hour
	// hitForMiss is how long requests skip the cache of a page whose result cannot be stored.
	hitForMiss = 2 * time.Minute
)

// wholePage is what an entry of the whole-page cache stands for besides the HTML.
type wholePage struct {
	// status is 200, or the status of a redirect to location. Only 200, 301, 302 and 308 are
	// stored, but the result of a request that waited for another may be any redirect.
	status   int
	location string
	// etag is the quoted tag of the HTML, without W/.
	etag string
	// routes holds the data of every Load of the page as JSON text, in the order of the chain.
	routes []wholeRoute
	// created is when the entry was made, expires when its time is over and staleUntil when it
	// stops standing in for a failed page.
	created, expires, staleUntil time.Time
}

// wholeRoute is the JSON text of the data of one Load, by route ID.
type wholeRoute struct{ id, text string }

func (w *wholePage) size() int64 {
	n := int64(len(w.location) + len(w.etag) + 128)
	for _, r := range w.routes {
		n += int64(len(r.id) + len(r.text) + 32)
	}
	return n
}

// storableStatus reports whether an entry of this status is kept.
func storableStatus(status int) bool {
	return status == http.StatusOK || status == http.StatusMovedPermanently || status == http.StatusFound || status == http.StatusPermanentRedirect
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

// cachedPageRequest answers a page with the whole-page cache: GET, HEAD or the navigation data of
// it, with the entry of its cleaned URL, which it makes when there is none that is fresh.
func (s *Server) cachedPageRequest(c *tifa.Ctx, route Route, chain []load, ttl time.Duration) error {
	rq := c.Request()
	var selected []load
	if c.IsData() {
		var err error
		if selected, err = s.listedLoads(c, chain); err != nil {
			return err
		}
	}
	// The render holds the lock for reading, so the pool it uses is not closed meanwhile.
	f := s.front.Load()
	key := s.pageKey(rq, c.Pattern(), f)
	appStorable := headersStorable(c.Writer().Header())
	base := rq.WithContext(rq.Context())
	pattern := c.Pattern()
	generate := func(ctx context.Context) (cachedPage, bool, error) {
		return s.generate(ctx, base, pattern, route, chain, f, key, ttl, appStorable)
	}
	var got fetched
	var err error
	if s.cache.bypassed(key, cacheNow()) {
		var page cachedPage
		page, _, err = generate(rq.Context())
		got = fetched{page: page, how: "vm"}
	} else {
		unheard := func(err error) {
			core.Logger().ErrorContext(c.Context(), "tifa: a page was being made for the cache and no request waits for it", "method", rq.Method, "path", rq.URL.Path, "err", err)
		}
		got, err = s.cache.fetch(rq.Context(), key, unheard, generate)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) && rq.Context().Err() != nil {
			return errClientGone
		}
		old, ok := s.cache.stale(key, cacheNow())
		if !ok || !regenerationFailed(err) {
			return s.pageFailure(c, err, selected)
		}
		core.Logger().WarnContext(c.Context(), "tifa: making the page failed, the old page is served", "method", rq.Method, "path", rq.URL.Path, "err", err)
		got = fetched{page: old, held: true, how: "cache"}
	}
	return s.respondPage(c, got, selected)
}

// regenerationFailed reports whether err means making a page failed, as opposed to the answer of
// the page: a redirect, a 404 or another error the app returns on purpose is the page.
func regenerationFailed(err error) bool {
	var lf *loadFailure
	if errors.As(err, &lf) {
		return loadFailed(lf.errs[lf.failed])
	}
	var re *renderError
	return errors.As(err, &re) || errors.Is(err, tifa.ErrSSRBusy) || errors.Is(err, context.DeadlineExceeded)
}

// loadFailed reports whether the error of a Load is a failure and not an answer: anything but a
// redirect, tifa.ErrNotFound, field errors and a tifa.Error below 500.
func loadFailed(err error) bool {
	var redirect *core.RedirectError
	var e tifa.Error
	switch {
	case errors.As(err, &redirect), errors.Is(err, tifa.ErrNotFound), isFields(err):
		return false
	case asPublic(err, &e):
		return e.Status >= http.StatusInternalServerError
	}
	return true
}

// pageFailure turns the error of making a page into the error this request answers. The failure of
// the Loads is the one of the whole chain, and the request answers for the Loads it asked for.
func (s *Server) pageFailure(c *tifa.Ctx, err error, selected []load) error {
	var lf *loadFailure
	if !errors.As(err, &lf) {
		return err
	}
	out := *lf
	out.data = c.IsData()
	if !out.data {
		return &out
	}
	asked := map[string]bool{}
	for _, l := range selected {
		asked[l.id] = true
	}
	out.chain, out.values, out.errs, out.failed = nil, nil, nil, -1
	for i, l := range lf.chain {
		if !asked[l.id] {
			continue
		}
		if out.failed < 0 && lf.errs[i] != nil {
			out.failed = len(out.chain)
		}
		out.chain = append(out.chain, l)
		out.values = append(out.values, lf.values[i])
		out.errs = append(out.errs, lf.errs[i])
	}
	if out.failed >= 0 {
		return &out
	}
	// Every Load it asked for succeeded: the failure is another one's, and it has its data.
	items := make(map[string]routeResult, len(out.chain))
	for i, l := range out.chain {
		items[l.id] = routeResult{Status: http.StatusOK, Data: out.values[i]}
	}
	return s.writeDataAnswer(c, items)
}

// generate makes the entry of key: it runs the Loads of the whole chain, renders the page and
// returns it, and whether the cache keeps it. A result that is not the page, a 404 or another
// answer of the app, is returned as an error for each request to answer with. Requests skip the
// cache of key for a while after a result that cannot be stored.
func (s *Server) generate(ctx context.Context, base *http.Request, pattern string, route Route, chain []load, f *front, key string, ttl time.Duration, appStorable bool) (cachedPage, bool, error) {
	rq := base.Clone(ctx)
	rq.URL.RawQuery = s.ignore.clean(rq.URL.RawQuery, s.pageQuery[pattern])
	rq.RequestURI = rq.URL.RequestURI()
	headers := http.Header{}
	c := core.New(&headerOnly{h: headers}, rq, pattern, s.cfg.BodyLimit)
	core.SetAnonymous(c)
	created := cacheNow()
	entry := &wholePage{created: created, expires: created.Add(ttl), staleUntil: created.Add(ttl + staleIfError)}

	items, err := runChain(c, chain, route.SSR, route.Path)
	if err != nil {
		var lf *loadFailure
		if errors.As(err, &lf) {
			var redirect *core.RedirectError
			if errors.As(lf.errs[lf.failed], &redirect) {
				entry.status, entry.location = redirect.Code, redirectLocation(rq, redirect.URL, redirect.Code)
				return s.redirectEntry(key, entry, appStorable, headers)
			}
			if !loadFailed(lf.errs[lf.failed]) {
				// An answer of the app, a 404 for one, replaces the page that was kept.
				s.cache.markBypass(key, created.Add(hitForMiss))
				s.cache.remove(key)
			}
		}
		return cachedPage{}, false, err
	}
	for _, l := range chain {
		text, err := loaderText(items[l.id].Data)
		if err != nil {
			return cachedPage{}, false, err
		}
		entry.routes = append(entry.routes, wholeRoute{id: l.id, text: text})
	}

	var page cachedPage
	var held bool
	sess := pageSession{}
	if s.sess.enabled {
		k := s.sessionKey()
		sess = pageSession{key: k, head: sessionHead(k)}
	}
	if !route.SSR {
		doc, err := dataOnlyDocument(f, items, route.Path, s.pageURI(rq, pattern), sess)
		if err != nil {
			return cachedPage{}, false, err
		}
		page, held = cachedPage{html: doc}, s.cache.holds(doc)
		if held && s.cfg.Compress {
			page.gzip, page.zstd = compressPage(doc)
		}
		entry.etag = contentTag(doc)
	} else {
		data, texts, err := renderInput(items)
		if err != nil {
			return cachedPage{}, false, err
		}
		s.rendering.RLock()
		defer s.rendering.RUnlock()
		in := ssr.Input{BuildID: f.build.ID, URL: s.pageURI(rq, pattern), Data: data, Texts: texts, Preload: f.preloadOf(route.Path, items),
			SessionKey: sess.key, SessionHead: sess.head}
		failed := &renderError{f: f, in: in, key: route.Path, start: time.Now()}
		page, held, err = s.renderEntry(ctx, rq, f, in, route)
		if err != nil {
			var redirect *ssr.RedirectError
			if errors.As(err, &redirect) {
				entry.status, entry.location = redirect.Status, redirect.Location
				return s.redirectEntry(key, entry, appStorable, headers)
			}
			failed.err = err
			return cachedPage{}, false, failed
		}
		entry.etag = renderKey(in, f.build.bundle)
		if route.NoRenderCache {
			entry.etag = contentTag(page.html)
		}
	}
	entry.status = http.StatusOK
	page.whole = entry
	storable := appStorable && headersStorable(headers)
	if !storable {
		s.cache.markBypass(key, created.Add(hitForMiss))
	}
	return page, held && storable, nil
}

// redirectEntry returns the entry of a redirect. It is kept when its status is one that is stored and
// the headers of the app allow it. Otherwise requests skip the cache of the page for a while.
func (s *Server) redirectEntry(key string, entry *wholePage, appStorable bool, headers http.Header) (cachedPage, bool, error) {
	storable := storableStatus(entry.status) && appStorable && headersStorable(headers)
	if !storable {
		s.cache.markBypass(key, entry.created.Add(hitForMiss))
	}
	return cachedPage{whole: entry}, storable, nil
}

// renderEntry renders the page of an entry, through the render cache unless the page turned it
// off, and returns it as the cache keeps it.
func (s *Server) renderEntry(ctx context.Context, rq *http.Request, f *front, in ssr.Input, route Route) (cachedPage, bool, error) {
	method, path := rq.Method, rq.URL.Path
	if route.NoRenderCache {
		return s.renderOne(ctx, f, in, false, method, path)
	}
	unheard := func(err error) {
		if !errors.As(err, new(*ssr.RedirectError)) {
			core.Logger().ErrorContext(ctx, "tifa: a render failed and no request waits for it", "method", method, "path", path, "err", &renderError{err: err, f: f, in: in})
		}
	}
	got, err := s.cache.fetch(ctx, renderKey(in, f.build.bundle), unheard, func(ctx context.Context) (cachedPage, bool, error) {
		return s.renderOne(ctx, f, in, true, method, path)
	})
	if err != nil {
		return cachedPage{}, false, err
	}
	return got.page, got.held, nil
}

// contentTag is the quoted tag of a page made from its content, for a page that has no
// render key to stand on.
func contentTag(html string) string {
	sum := sha256.Sum256([]byte(html))
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// respondPage answers a request from an entry.
func (s *Server) respondPage(c *tifa.Ctx, got fetched, selected []load) error {
	entry := got.page.whole
	if c.IsData() {
		c.SetHeader(renderHeader, got.how)
		if entry.status != http.StatusOK {
			session := s.answerSession(c)
			if session != nil {
				s.sealSessionAnswer(c)
			}
			return writeDataBody(c.Writer(), http.StatusOK, dataRedirect{Redirect: entry.location, Status: entry.status, Session: session})
		}
		items := make(map[string]routeResult, len(selected))
		for _, l := range selected {
			i := slices.IndexFunc(entry.routes, func(r wholeRoute) bool { return r.id == l.id })
			result := routeResult{Status: http.StatusOK}
			if text := entry.routes[i].text; text != "null" {
				result.Data = json.RawMessage(text)
			}
			items[l.id] = result
		}
		return s.writeDataAnswer(c, items)
	}
	c.SetHeader(renderHeader, got.how)
	if entry.status != http.StatusOK {
		http.Redirect(c.Writer(), c.Request(), entry.location, entry.status)
		return nil
	}
	etag := entry.etag
	if s.cfg.Compress {
		etag = "W/" + etag
	}
	c.SetHeader("ETag", etag)
	return writeCached(c, http.StatusOK, got.page, s.formEncoding(c))
}

// writeDataAnswer writes the answer of a navigation: the results given, and the session of the browser.
func (s *Server) writeDataAnswer(c *tifa.Ctx, items map[string]routeResult) error {
	session, err := s.browserSession(c)
	if err != nil {
		return err
	}
	if session != nil {
		if err := checkSessionCache(c.Writer().Header()); err != nil {
			return err
		}
	}
	return writeDataBody(c.Writer(), http.StatusOK, dataAnswer{Routes: items, Session: session})
}
