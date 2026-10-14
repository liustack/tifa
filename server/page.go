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
	"log"
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
	ID     string `json:"buildId"`
	Script string `json:"script"`
	Style  string `json:"style"`
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
	if len(s.pages) > 0 && (render == nil || s.build.Script == "") {
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

// loadFailure is what a page returns when a Load of its chain failed: the outermost failure,
// already sorted by classify, and the results of the whole chain for the error page.
type loadFailure struct {
	err      error
	redirect *core.RedirectError
	public   tifa.Error
	items    map[string]routeResult
	ssr      bool
}

func (f *loadFailure) Error() string { return f.err.Error() }
func (f *loadFailure) Unwrap() error { return f.err }

// renderError is a failed SSR render. The error page then comes from Go, not from the VM.
type renderError struct{ err error }

func (e *renderError) Error() string { return "tifa: render page: " + e.err.Error() }
func (e *renderError) Unwrap() error { return e.err }

// results turns the outcomes of the Loads into routeResults by route ID, sorting each error
// with classify. Loads that share a failure through tifa.Use return the same error, which
// classify sees once, so OnError reports it once. failed is the index of the outermost failure,
// -1 when every Load succeeded.
func (s *Server) results(c *tifa.Ctx, chain []load, values []any, errs []error) (items map[string]routeResult, failed int, redirect *core.RedirectError, public tifa.Error) {
	items = make(map[string]routeResult, len(chain))
	failed = -1
	type sorted struct {
		err      error
		redirect *core.RedirectError
		public   tifa.Error
	}
	var seen []sorted
	for i, l := range chain {
		err := errs[i]
		if err == nil {
			items[l.id] = routeResult{Status: http.StatusOK, Data: values[i]}
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
		if failed < 0 {
			failed, redirect, public = i, x.redirect, x.public
		}
	}
	return items, failed, redirect, public
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
		values, errs := runLoads(c, chain)
		items, failed, redirect, public := s.results(c, chain, values, errs)
		switch {
		case failed >= 0:
			return &loadFailure{err: errs[failed], redirect: redirect, public: public, items: items, ssr: r.SSR}
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
	values, errs := runLoads(c, selected)
	items, _, _, _ := s.results(c, selected, values, errs)
	// The Loads have returned, the header map is no longer shared. A Cache-Control of
	// OnRequest or a Load, such as private, no-store, wins over the default.
	if c.Writer().Header().Get("Cache-Control") == "" {
		c.Writer().Header().Set("Cache-Control", "no-cache")
	}
	return c.JSON(http.StatusOK, items)
}

// renderPage renders the document in the VM. A successful page gets an ETag from the hash of
// the build ID, the URL and the data, which are all the render depends on. A matching
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
		sum := sha256.Sum256([]byte(in.BuildID + "\x00" + in.URL + "\x00" + string(data)))
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
// the data, so the browser renders it without a data request (Boot in data.ts).
func (s *Server) dataOnly(c *tifa.Ctx, items map[string]routeResult, status int) error {
	boot, err := json.Marshal(map[string]any{
		"url": c.Request().URL.RequestURI(), "buildId": s.build.ID, "data": items,
		"assets": map[string]string{"script": s.build.Script, "style": s.build.Style},
	})
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">`)
	if s.build.Style != "" {
		fmt.Fprintf(&b, `<link rel="stylesheet" href="%s">`, html.EscapeString(s.build.Style))
	}
	fmt.Fprintf(&b, `<script type="module" src="%s"></script></head><body>`, html.EscapeString(s.build.Script))
	// json.Marshal escapes <, > and &, so the data cannot close the script element.
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
//   - a Load of the chain failed: the page with the results of the whole chain
//   - no route matched: the root Layout's NotFound, which needs no data
//
// Anything else, a failed OnRequest for one, gets a plain page from Go.
func (s *Server) errorPage(c *tifa.Ctx, e tifa.Error, failure *loadFailure) {
	r := c.Request()
	var items map[string]routeResult
	var ssrOn bool
	switch {
	case failure != nil:
		items, ssrOn = failure.items, failure.ssr
	case c.Pattern() == "" && e.Status == http.StatusNotFound:
		items, ssrOn = map[string]routeResult{}, s.rootSSR
	default:
		writeFallback(c.Writer(), e)
		return
	}
	var err error
	if ssrOn {
		err = s.renderPage(c, items, e.Status, false)
	} else {
		err = s.dataOnly(c, items, e.Status)
	}
	if err != nil {
		log.Printf("tifa: %s %s: render the %d page: %v", r.Method, r.URL.Path, e.Status, err)
		writeFallback(c.Writer(), e)
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
