package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/core"
	"github.com/liustack/tifa/internal/ssr"
)

// Headers of the navigation data protocol, see js/runtime/data.ts.
const (
	routeHeader  = "X-Tifa-Route"
	buildHeader  = "X-Tifa-Build"
	renderHeader = "X-Tifa-Render" // how a page was answered: vm, cache or none
)

// frontBuild is dist/build.json.
type frontBuild struct {
	ID string `json:"buildId"`
	// Head is the HTML that loads the browser bundle: stylesheets, module preloads and the entry
	// script, or under tifa dev the scripts of Vite's dev server.
	Head string `json:"head"`
	// bundle is the hash of ssr.js. tifa dev keeps the build ID when only ssr.js changes, so
	// what a render depends on is the build ID and the bundle.
	bundle string
}

// setFront serves pages from dist: dist/build.json names the build and its assets, dist/assets
// and dist/public are served as files. render runs the SSR bundle, nil when the application has
// no pages.
func (s *Server) setFront(dist fs.FS, render func(context.Context, ssr.Input) (string, error)) error {
	data, err := fs.ReadFile(dist, "build.json")
	if err != nil {
		return fmt.Errorf("tifa: read the build: %w", err)
	}
	if err := json.Unmarshal(data, &s.build); err != nil {
		return fmt.Errorf("tifa: build.json: %w", err)
	}
	if s.build.ID == "" {
		return errors.New("tifa: build.json has no build ID")
	}
	if render != nil {
		source, err := fs.ReadFile(dist, "ssr.js")
		if err != nil {
			return err
		}
		sum := sha256.Sum256(source)
		s.build.bundle = hex.EncodeToString(sum[:16])
	}
	for _, dir := range []struct {
		name string
		fsys *fs.FS
	}{{"assets", &s.assets}, {"public", &s.public}} {
		if _, err := fs.Stat(dist, dir.name); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		sub, err := fs.Sub(dist, dir.name)
		if err != nil {
			return err
		}
		*dir.fsys = sub
	}
	if len(s.pages) > 0 && (render == nil || s.build.Head == "") {
		return errors.New("tifa: the application has pages but the build has no bundles")
	}
	s.render = render
	// Without pages there is nothing to render, and unmatched URLs keep their JSON answer.
	s.front = len(s.pages) > 0
	return nil
}

// openFront serves the front end in dist, the dist/ directory tifa build writes. With an SSR
// bundle (ssr.js) it starts the VM pool that renders pages and returns it, to be closed after
// the server stops.
func (s *Server) openFront(dist fs.FS, cfg config.SSR) (*ssr.Pool, error) {
	source, err := fs.ReadFile(dist, "ssr.js")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, s.setFront(dist, nil)
	}
	if err != nil {
		return nil, err
	}
	pool, err := ssr.NewPool(string(source), cfg)
	if err != nil {
		return nil, err
	}
	if err := s.setFront(dist, pool.Render); err != nil {
		return nil, errors.Join(err, pool.Close())
	}
	return pool, nil
}

// serveStatic answers GET and HEAD for a file of the browser bundle (/assets/...) or of the
// public directory, before routing and without OnRequest. It reports whether it answered.
// Bundle files carry a content hash in their names and are cached for good. A name under
// /assets/ that the bundle lacks may still be a public file, such as public/assets/logo.svg.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" && r.Method != "HEAD" {
		return false
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if rest, ok := strings.CutPrefix(name, "assets/"); ok && serveFile(w, r, s.assets, rest, "public, max-age=31536000, immutable") {
		return true
	}
	return serveFile(w, r, s.public, name, "no-cache")
}

// serveFile answers with the regular file name of fsys and reports whether it exists.
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name, cache string) bool {
	if fsys == nil || name == "" || !fs.ValidPath(name) {
		return false
	}
	if info, err := fs.Stat(fsys, name); err != nil || !info.Mode().IsRegular() {
		return false
	}
	w.Header().Set("Cache-Control", cache)
	http.ServeFileFS(w, r, fsys, name)
	return true
}

// routeResult is the outcome of one Load as the browser reads it (RouteResult in data.ts).
type routeResult struct {
	Status   int    `json:"status"`
	Data     any    `json:"data,omitempty"`
	Error    string `json:"error,omitempty"`
	Location string `json:"location,omitempty"`
}

// loadFailure is what a page returns from Next when a Load of the loaded routes failed. Its
// error is the outermost failure, the one OnRequest sees. The Loads' errors are sorted only
// once OnRequest has returned it unchanged (see Server.fail), so OnError never sees an error
// the middleware handles.
type loadFailure struct {
	chain  []load
	values []any
	errs   []error
	failed int  // index of the outermost failure in chain
	data   bool // a data request, answered by route as JSON
	ssr    bool
}

func (f *loadFailure) Error() string { return f.errs[f.failed].Error() }
func (f *loadFailure) Unwrap() error { return f.errs[f.failed] }

// renderError is a failed SSR render. The error page then comes from Go, not from the VM.
type renderError struct{ err error }

func (e *renderError) Error() string { return "tifa: render page: " + e.err.Error() }
func (e *renderError) Unwrap() error { return e.err }

// runChain runs the Loads of chain and returns their results by route ID, or a *loadFailure
// when one failed.
func runChain(c *tifa.Ctx, chain []load, ssr bool) (map[string]routeResult, error) {
	values, errs := runLoads(c, chain)
	for i, err := range errs {
		if err != nil {
			return nil, &loadFailure{chain: chain, values: values, errs: errs, failed: i, data: c.IsData(), ssr: ssr}
		}
	}
	items := make(map[string]routeResult, len(chain))
	for i, l := range chain {
		items[l.id] = routeResult{Status: http.StatusOK, Data: values[i]}
	}
	return items, nil
}

// results sorts the errors of a failure with classify and returns the results by route ID and
// the sorted outermost failure. Loads that share a failure through tifa.Use return the same
// error, which classify sees once, so OnError reports it once.
func (s *Server) results(c *tifa.Ctx, f *loadFailure) (items map[string]routeResult, redirect *core.RedirectError, public tifa.Error) {
	items = make(map[string]routeResult, len(f.chain))
	type sorted struct {
		err      error
		redirect *core.RedirectError
		public   tifa.Error
	}
	var seen []sorted
	for i, l := range f.chain {
		err := f.errs[i]
		if err == nil {
			items[l.id] = routeResult{Status: http.StatusOK, Data: f.values[i]}
			continue
		}
		j := slices.IndexFunc(seen, func(x sorted) bool { return sameError(x.err, err) })
		if j < 0 {
			r, e := s.classify(c, err)
			seen = append(seen, sorted{err, r, e})
			j = len(seen) - 1
		}
		x := seen[j]
		if x.redirect != nil {
			items[l.id] = routeResult{Status: x.redirect.Code, Location: x.redirect.URL}
		} else {
			items[l.id] = routeResult{Status: x.public.Status, Error: x.public.Message}
		}
		if i == f.failed {
			redirect, public = x.redirect, x.public
		}
	}
	return items, redirect, public
}

// sameError reports whether a and b are the same error value, without panicking on error types
// that cannot be compared.
func sameError(a, b error) bool {
	t := reflect.TypeOf(a)
	return t == reflect.TypeOf(b) && t.Comparable() && a == b
}

// pageHandler answers GET for a page:
//   - with X-Tifa-Route, the data of the listed routes as JSON
//   - with SSR on its chain, the HTML the VM renders, or 304, or the cached HTML for Cache = true
//   - with SSR = false somewhere on its chain, a minimal document carrying the data
//
// When a Load fails it returns a *loadFailure, for OnRequest to see as the error of Next.
// Without a front end it answers the chain's data as JSON by route ID.
func (s *Server) pageHandler(r Route, chain []load) func(*tifa.Ctx) error {
	return func(c *tifa.Ctx) error {
		if !s.front {
			return loadsAsJSON(c, chain)
		}
		// Added, not set: the Vary of OnRequest, such as Cookie, must stay for caches to keep
		// the answers of two users apart. No Load runs yet, so the header map is not shared.
		c.Writer().Header().Add("Vary", routeHeader)
		if c.IsData() {
			return s.dataRequest(c, chain)
		}
		items, err := runChain(c, chain, r.SSR)
		switch {
		case err != nil:
			return err
		case !r.SSR:
			return s.dataOnly(c, items, http.StatusOK)
		}
		return s.renderPage(c, items, http.StatusOK, r.Cache)
	}
}

// loadsAsJSON answers a page of an application built without a front end, as in the tests of
// this package: the results of every Load in the chain as JSON by route ID. With several errors
// the one of the outermost Load wins, so the answer does not depend on timing.
func loadsAsJSON(c *tifa.Ctx, chain []load) error {
	values, errs := runLoads(c, chain)
	data := make(map[string]any, len(chain))
	for i, l := range chain {
		if errs[i] != nil {
			return errs[i]
		}
		data[l.id] = values[i]
	}
	return c.JSON(http.StatusOK, data)
}

// dataRequest answers a client navigation: the results of the routes listed in X-Tifa-Route,
// each with its own status. A browser running another build gets 409 and reloads the page.
// When a listed Load fails, Next returns the failure as it does for the page, and the answer
// is written once OnRequest has returned (see Server.fail).
func (s *Server) dataRequest(c *tifa.Ctx, chain []load) error {
	if c.Header(buildHeader) != s.build.ID {
		return tifa.Error{Status: http.StatusConflict, Message: "the application was updated, reload the page"}
	}
	var selected []load
	for _, id := range strings.Split(c.Header(routeHeader), ",") {
		id = strings.TrimSpace(id)
		i := slices.IndexFunc(chain, func(l load) bool { return l.id == id })
		if i < 0 {
			return tifa.Error{Status: http.StatusBadRequest, Message: fmt.Sprintf("route %q has no data at this URL", id)}
		}
		if !slices.ContainsFunc(selected, func(l load) bool { return l.id == id }) {
			selected = append(selected, chain[i])
		}
	}
	items, err := runChain(c, selected, false)
	if err != nil {
		return err
	}
	return writeData(c.Writer(), http.StatusOK, items)
}

// writeData writes the answer of a data request. A Cache-Control of OnRequest or a Load, such
// as private, no-store, wins over the default. The Loads have returned, so the header map is
// no longer shared.
func writeData(w http.ResponseWriter, status int, value any) error {
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, err = w.Write(data)
	return err
}

// dataRedirect is how a data request answers a redirect: 200 and this JSON, which the
// collector turns into a router redirect. A 3xx would be followed by fetch, still asking for
// the routes of the old page.
type dataRedirect struct {
	Redirect string `json:"redirect"`
	Status   int    `json:"status"`
}

// renderPage renders the document in the VM. A successful page gets an ETag from the hash of
// the build ID, the SSR bundle, the URL and the data, which are all the render depends on. A matching
// If-None-Match is answered 304, and with cache the HTML of an earlier render is reused.
func (s *Server) renderPage(c *tifa.Ctx, items map[string]routeResult, status int, cache bool) error {
	r := c.Request()
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	in := ssr.Input{BuildID: s.build.ID, URL: r.URL.RequestURI(), Data: data}
	var key string
	if status == http.StatusOK {
		sum := sha256.Sum256([]byte(in.BuildID + "\x00" + s.build.bundle + "\x00" + in.URL + "\x00" + string(data)))
		key = `"` + hex.EncodeToString(sum[:16]) + `"`
		c.SetHeader("ETag", key)
		if etagMatches(c.Header("If-None-Match"), key) {
			c.SetHeader(renderHeader, "none")
			return c.NoContent(http.StatusNotModified)
		}
		if cache {
			if page, ok := s.cache.get(key); ok {
				c.SetHeader(renderHeader, "cache")
				return writeHTML(c, status, page)
			}
		}
	}
	page, err := s.render(r.Context(), in)
	var redirect *ssr.RedirectError
	if errors.As(err, &redirect) {
		// The router spells the URL differently, /%61 for /a, and sends the browser there.
		// The redirect carries no ETag. The render is a function of the build, the URL and the
		// data, so an ETag a client got with a 200 for this URL never belongs to a redirect,
		// and 304 is only answered to a client that made its If-None-Match up.
		c.Writer().Header().Del("ETag")
		http.Redirect(c.Writer(), r, redirect.Location, redirect.Status)
		return nil
	}
	if err != nil {
		return &renderError{err: err}
	}
	if cache && key != "" {
		s.cache.put(key, page)
	}
	c.SetHeader(renderHeader, "vm")
	return writeHTML(c, status, page)
}

// etagMatches implements the If-None-Match comparison of RFC 9110: weak, any of a list, or *.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

// dataOnly answers a page that skips SSR: a document that loads the browser bundle and carries
// the data, so the browser renders it without a data request (Boot in data.ts). The browser
// renders into #tifa-root and writes the head of the routes, the viewport meta among it.
func (s *Server) dataOnly(c *tifa.Ctx, items map[string]routeResult, status int) error {
	boot, err := json.Marshal(map[string]any{
		"url": c.Request().URL.RequestURI(), "buildId": s.build.ID, "data": items,
	})
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	b.WriteString(s.build.Head)
	b.WriteString(`</head><body><div id="tifa-root"></div>`)
	// json.Marshal escapes <, >, &, U+2028 and U+2029, so the data cannot close the script element.
	fmt.Fprintf(&b, `<script type="application/json" id="tifa-data">%s</script></body></html>`, boot)
	c.SetHeader(renderHeader, "none")
	return writeHTML(c, status, b.String())
}

func writeHTML(c *tifa.Ctx, status int, page string) error {
	w := c.Writer()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := w.Write([]byte(page))
	return err
}

// errorPage answers a failed page request with a page and the error's status. The document
// survives any failure, so the router shows the right level's NotFound or Error component:
//   - a Load of the chain failed: the page with the results of the whole chain (items)
//   - no route matched: the root Layout's NotFound, which needs no data
//
// Anything else, a failed OnRequest for one, gets a plain page from Go. So does a page whose
// render fails, after OnError has seen that failure. The plain page is not rendered, so a
// failing error page ends there.
func (s *Server) errorPage(c *tifa.Ctx, e tifa.Error, items map[string]routeResult, ssrOn bool) {
	if items == nil {
		if c.Pattern() != "" || e.Status != http.StatusNotFound {
			writeFallback(c.Writer(), e)
			return
		}
		items, ssrOn = map[string]routeResult{}, s.rootSSR
	}
	var err error
	if ssrOn {
		err = s.renderPage(c, items, e.Status, false)
	} else {
		err = s.dataOnly(c, items, e.Status)
	}
	if err != nil {
		writeFallback(c.Writer(), s.publicError(c, fmt.Errorf("render the %d page: %w", e.Status, err)))
	}
}

// writeFallback writes a plain error page when the application cannot render one.
func writeFallback(w http.ResponseWriter, e tifa.Error) {
	title := html.EscapeString(fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status)))
	page := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>`,
		title, title, html.EscapeString(e.Message))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(e.Status)
	_, _ = w.Write([]byte(page))
}
