package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
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
	// headers are the response headers the Load and Layout functions set, except those the
	// framework writes itself (ownedHeader). Cache-Control and Vary are here only when they make the
	// answer not for everyone, and then the page is not kept and these headers go out with it.
	headers http.Header
	// fx is what making the entry does to the cache besides storing it.
	fx effects
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
	for name, values := range w.headers {
		n += int64(len(name)) + 32
		for _, v := range values {
			n += int64(len(v)) + 16
		}
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
func (s *server) cachedPageRequest(c *tifa.Ctx, route Route, chain []load, ttl time.Duration) error {
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
	// A request whose own OnRequest made the answer not for everyone neither reads nor feeds the
	// cache: it makes its page alone.
	own := !headersStorable(c.Writer().Header())
	base := rq.WithContext(rq.Context())
	pattern := c.Pattern()
	generate := func(ctx context.Context) (cachedPage, bool, error) {
		return s.generate(ctx, base, pattern, route, chain, f, ttl)
	}
	alone := func() (fetched, error) {
		page, _, err := generate(rq.Context())
		return fetched{page: page, how: "vm"}, err
	}
	var got fetched
	var err error
	status := "tifa; hit"
	switch {
	case own || s.cache.bypassed(key, cacheNow()):
		got, err = alone()
		status = "tifa; fwd=bypass"
	default:
		unheard := func(err error) {
			core.Logger().ErrorContext(c.Context(), "tifa: a page was being made for the cache and no request waits for it", "method", rq.Method, "path", rq.URL.Path, "err", err)
		}
		got, err = s.cache.fetch(rq.Context(), key, unheard, generate)
		if got.how == "retry" {
			// What the request that made the page got is not for everyone, so this one makes its own.
			got, err = alone()
		}
		if got.how != "cache" {
			status = "tifa; fwd=miss"
		}
	}
	if err != nil {
		if errors.Is(err, context.Canceled) && rq.Context().Err() != nil {
			return errClientGone
		}
		old, ok := s.cache.stale(key, cacheNow())
		if !ok || !regenerationFailed(err) {
			var made *madeError
			if errors.As(err, &made) {
				replayHeaders(c.Writer().Header(), made.headers)
				err = made.err
			}
			return s.pageFailure(c, err, selected)
		}
		core.Logger().WarnContext(c.Context(), "tifa: making the page failed, the old page is served", "method", rq.Method, "path", rq.URL.Path, "err", err)
		got, status = fetched{page: old, held: true, how: "cache"}, "tifa; fwd=stale"
	}
	return s.respondPage(c, got, status, selected)
}

// ownedHeader reports whether the framework writes the header itself on a page of the whole-page
// cache, so a Load or Layout cannot keep it in an entry.
func ownedHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Set-Cookie", "Cache-Control", "Vary", "Age", "Etag", "Content-Length", "Content-Encoding", "Content-Type":
		return true
	}
	return false
}

// keptHeaders returns the headers of a make to keep with the entry and to send with every answer made
// from it, and whether they say the answer is not for everyone. A Cache-Control and a Vary count only
// when they say so.
func keptHeaders(h http.Header) (kept http.Header, unshared bool) {
	unshared = !headersStorable(h)
	kept = http.Header{}
	hop := hopByHop(h)
	for name, values := range h {
		if hop[http.CanonicalHeaderKey(name)] {
			continue
		}
		if !ownedHeader(name) || unshared && (name == "Cache-Control" || name == "Vary") {
			kept[name] = slices.Clone(values)
		}
	}
	return kept, unshared
}

// hopByHop returns the names of the headers that belong to one connection and are not kept in an
// entry or sent again from it (RFC 9111 section 3.1): Connection and the fields it lists, Keep-Alive,
// TE, Trailer, Transfer-Encoding, Upgrade, Proxy-Authenticate, Proxy-Authorization and Proxy-Connection.
func hopByHop(h http.Header) map[string]bool {
	hop := map[string]bool{}
	for _, name := range []string{"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		hop[http.CanonicalHeaderKey(name)] = true
	}
	for name, values := range h {
		if http.CanonicalHeaderKey(name) == "Connection" {
			for _, v := range values {
				for _, field := range strings.Split(v, ",") {
					if field = strings.TrimSpace(field); field != "" {
						hop[http.CanonicalHeaderKey(field)] = true
					}
				}
			}
		}
	}
	return hop
}

// mergeCacheControl merges the Cache-Control fields of the two sides by directive. no-store alone
// wins over everything. private and no-cache are separate limits and each stays when either side has it,
// without a list of fields when either side has it so (that is the stronger). max-age and s-maxage take the
// smaller, and the other directives are the union. A field that cannot be read counts as no-store.
func mergeCacheControl(a, b http.Header) string {
	type limit struct {
		present, bare bool
		fields        []string
	}
	var private, noCache limit
	maxAge, sMaxAge := int64(-1), int64(-1)
	var others []string
	seen := map[string]bool{}
	for _, h := range []http.Header{a, b} {
		for _, value := range h.Values("Cache-Control") {
			directives, ok := splitDirectives(value)
			if !ok {
				return "no-store"
			}
			for _, d := range directives {
				name, arg, hasArg := strings.Cut(d, "=")
				name = strings.ToLower(strings.TrimSpace(name))
				arg = strings.Trim(strings.TrimSpace(arg), `"`)
				switch name {
				case "no-store":
					return "no-store"
				case "private", "no-cache":
					l := &private
					if name == "no-cache" {
						l = &noCache
					}
					l.present = true
					if !hasArg {
						l.bare = true
					} else {
						for _, f := range strings.Split(arg, ",") {
							if f = strings.TrimSpace(f); f != "" && !slices.Contains(l.fields, f) {
								l.fields = append(l.fields, f)
							}
						}
					}
				case "max-age", "s-maxage":
					n, err := strconv.ParseInt(arg, 10, 64)
					if err != nil || n < 0 {
						continue
					}
					p := &maxAge
					if name == "s-maxage" {
						p = &sMaxAge
					}
					if *p < 0 || n < *p {
						*p = n
					}
				default:
					if !seen[name] {
						seen[name] = true
						others = append(others, d)
					}
				}
			}
		}
	}
	var out []string
	for _, l := range []struct {
		name string
		l    limit
	}{{"private", private}, {"no-cache", noCache}} {
		switch {
		case !l.l.present:
		case l.l.bare || len(l.l.fields) == 0:
			out = append(out, l.name)
		default:
			out = append(out, l.name+`="`+strings.Join(l.l.fields, ", ")+`"`)
		}
	}
	if private.present && seen["public"] {
		others = slices.DeleteFunc(others, func(d string) bool { return strings.EqualFold(d, "public") })
	}
	slices.Sort(others)
	out = append(out, others...)
	if maxAge >= 0 {
		out = append(out, "max-age="+strconv.FormatInt(maxAge, 10))
	}
	if sMaxAge >= 0 {
		out = append(out, "s-maxage="+strconv.FormatInt(sMaxAge, 10))
	}
	return strings.Join(out, ", ")
}

// replayHeaders sets the headers a Load or Layout set on the response. The headers of OnRequest are
// already there. A Cache-Control that makes the answer not for everyone replaces a public one, and Vary
// only grows.
func replayHeaders(h http.Header, saved http.Header) {
	// The Cache-Control of OnRequest and of the Loads merge by directive (mergeCacheControl), whichever
	// was set first and whatever the order of the map. Vary is the union.
	vary := h.Values("Vary")
	for name, values := range saved {
		switch name {
		case "Cache-Control", "Vary":
		default:
			h[name] = slices.Clone(values)
		}
	}
	if len(saved["Cache-Control"]) > 0 {
		h.Set("Cache-Control", mergeCacheControl(h, saved))
	}
	if values := saved["Vary"]; len(values) > 0 {
		for _, v := range values {
			if !slices.Contains(vary, v) {
				vary = append(slices.Clone(vary), v)
			}
		}
		h["Vary"] = vary
	}
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
func (s *server) pageFailure(c *tifa.Ctx, err error, selected []load) error {
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
func (s *server) generate(ctx context.Context, base *http.Request, pattern string, route Route, chain []load, f *front, ttl time.Duration) (cachedPage, bool, error) {
	rq := base.Clone(ctx)
	rq.URL.RawQuery = s.ignore.clean(rq.URL.RawQuery, s.pageQuery[pattern])
	rq.RequestURI = rq.URL.RequestURI()
	headers := http.Header{}
	c := core.New(&headerOnly{h: headers}, rq, pattern, s.cfg.BodyLimit)
	core.SetAnonymous(c)
	created := cacheNow()
	entry := &wholePage{created: created, expires: created.Add(ttl), staleUntil: created.Add(ttl + staleIfError)}

	items, loads, err := runChain(c, chain, route.SSR, route.Path)
	if err != nil {
		var lf *loadFailure
		if errors.As(err, &lf) {
			var redirect *core.RedirectError
			if errors.As(lf.errs[lf.failed], &redirect) {
				entry.status, entry.location = redirect.Status, redirectLocation(rq, redirect.URL, redirect.Status)
				return s.redirectEntry(entry, headers)
			}
			if !loadFailed(lf.errs[lf.failed]) {
				// An answer of the app, a 404 for one, replaces the page that was kept.
				return s.madeFailed(err, headers, effects{set: true, remove: true, bypass: true})
			}
		}
		return s.madeFailed(err, headers, effects{})
	}
	for _, l := range chain {
		text, err := loaderText(items[l.id].Data)
		if err != nil {
			return s.madeFailed(err, headers, effects{})
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
			return s.madeFailed(err, headers, effects{})
		}
		page, held = cachedPage{html: doc, body: new(bodyID)}, s.cache.holds(doc)
		if held && s.cfg.Compress {
			page.gzip, page.zstd = compressPage(doc)
		}
		entry.etag = contentTag(doc)
	} else {
		data, texts, err := renderInput(items)
		if err != nil {
			return s.madeFailed(err, headers, effects{})
		}
		s.rendering.RLock()
		defer s.rendering.RUnlock()
		in := ssr.Input{BuildID: f.build.ID, URL: s.pageURI(rq, pattern), Data: data, Texts: texts, Preload: f.preloadOf(route.Path, items),
			SessionKey: sess.key, SessionHead: sess.head}
		failed := &renderError{f: f, in: in, key: route.Path, start: time.Now(), loads: loads}
		page, held, err = s.renderEntry(ctx, rq, f, in, route)
		if err != nil {
			var redirect *ssr.RedirectError
			if errors.As(err, &redirect) {
				entry.status, entry.location = redirect.Status, redirect.Location
				return s.redirectEntry(entry, headers)
			}
			failed.err = err
			return s.madeFailed(failed, headers, effects{})
		}
		entry.etag = renderKey(in, f.build.bundle)
		if route.NoRenderCache {
			entry.etag = contentTag(page.html)
		}
	}
	entry.status = http.StatusOK
	entry.headers, entry.fx.unshared = keptHeaders(headers)
	storable := !entry.fx.unshared
	entry.fx.set, entry.fx.bypass = true, !storable
	page.whole = entry
	return page, held && storable, nil
}

// madeFailed returns the error of a make with the headers its Loads set and what it does to the cache.
func (s *server) madeFailed(err error, headers http.Header, fx effects) (cachedPage, bool, error) {
	kept, unshared := keptHeaders(headers)
	fx.set, fx.unshared = true, unshared
	return cachedPage{}, false, &madeError{err: err, headers: kept, fx: fx}
}

// redirectEntry returns the entry of a redirect. It is kept when its status is one that is stored and
// the headers of the app allow it. Otherwise requests skip the cache of the page for a while.
func (s *server) redirectEntry(entry *wholePage, headers http.Header) (cachedPage, bool, error) {
	entry.headers, entry.fx.unshared = keptHeaders(headers)
	storable := storableStatus(entry.status) && !entry.fx.unshared
	entry.fx.set, entry.fx.bypass = true, !storable
	return cachedPage{whole: entry}, storable, nil
}

// renderEntry renders the page of an entry, through the render cache unless the page turned it
// off, and returns it as the cache keeps it.
func (s *server) renderEntry(ctx context.Context, rq *http.Request, f *front, in ssr.Input, route Route) (cachedPage, bool, error) {
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

// respondPage answers a request from an entry. status is the Cache-Status of the answer.
func (s *server) respondPage(c *tifa.Ctx, got fetched, status string, selected []load) error {
	entry := got.page.whole
	replayHeaders(c.Writer().Header(), entry.headers)
	c.SetHeader("Cache-Status", status)
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
	now := cacheNow()
	if got.held {
		s.setCacheHeaders(c, entry, now)
	}
	c.SetHeader(renderHeader, got.how)
	if entry.status != http.StatusOK {
		http.Redirect(c.Writer(), c.Request(), entry.location, entry.status)
		return nil
	}
	// The data of the entry is as old as the entry, which was made just before its Loads ran. A held
	// entry goes out public, which a cache may keep, so only a page the cache did not keep says it.
	setDataAge(c.Writer().Header(), now.Sub(entry.created))
	etag := entry.etag
	if s.cfg.Compress {
		etag = "W/" + etag
	}
	c.SetHeader("ETag", etag)
	if etagMatches(c.Header("If-None-Match"), entry.etag) {
		return c.NoContent(http.StatusNotModified)
	}
	return writeCached(c, http.StatusOK, got.page, s.formEncoding(c))
}

// writeDataAnswer writes the answer of a navigation: the results given, and the session of the browser.
func (s *server) writeDataAnswer(c *tifa.Ctx, items map[string]routeResult) error {
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

// setCacheHeaders says how long the entry may be kept by a shared cache in front of tifa. The
// browser revalidates every time (max-age=0) and asks tifa, which answers 304 from the entry.
// s-maxage is the whole time of the page and Age the seconds since it was made, and a cache in front of
// tifa subtracts one from the other (RFC 9111), so the two together never keep a page longer than
// Cache. An old page that stands in for a failed one has an Age past s-maxage and is not fresh there.
func (s *server) setCacheHeaders(c *tifa.Ctx, entry *wholePage, now time.Time) {
	h := c.Writer().Header()
	age := max(0, int64(now.Sub(entry.created)/time.Second))
	h.Set("Age", strconv.FormatInt(age, 10))
	if !headersStorable(h) {
		return // the app asked for the answer not to be shared
	}
	ttl := int64(entry.expires.Sub(entry.created) / time.Second)
	h.Set("Cache-Control", "public, max-age=0, s-maxage="+strconv.FormatInt(ttl, 10)+", stale-if-error="+strconv.FormatInt(int64(staleIfError/time.Second), 10))
}
