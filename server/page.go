package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"maps"
	"mime"
	"net/http"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/klauspost/compress/gzhttp"
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/core"
	"github.com/liustack/tifa/internal/ssr"
)

// Headers of the navigation data protocol, see js/runtime/data.ts.
const (
	routeHeader  = "X-Tifa-Route"
	buildHeader  = "X-Tifa-Build"
	renderHeader = "X-Tifa-Render" // how a page was answered: vm, shared, cache or none
)

// Headers of a call of a method handler from the browser, see js/runtime/call.ts. The handler
// functions send callHeader, and a redirect then comes as 200, redirectHeader with its status,
// and a dataRedirect.
const (
	callHeader     = "X-Tifa-Call"
	redirectHeader = "X-Tifa-Redirect"
)

// frontBuild is dist/build.json.
type frontBuild struct {
	ID string `json:"buildId"`
	// Head is the HTML that loads the browser bundle: stylesheets, module preloads and the entry
	// script, or under tifa dev the scripts of Vite's dev server.
	Head string `json:"head"`
	// Preload lists the module preloads of the Layouts and routes by ID, see preloads.
	Preload map[string][]string `json:"preload"`
	// Public holds the content hash of each file of dist/public by its path there, which
	// serveFile turns into the ETag of the file. Absent under tifa dev.
	Public map[string]string `json:"public"`
	// bundle is the hash of ssr.js. tifa dev keeps the build ID when only ssr.js changes, so
	// what a render depends on is the build ID and the bundle.
	bundle string
}

// front is what the server serves from a dist directory. A Server swaps it as a whole, see
// swapFront.
type front struct {
	build  frontBuild
	assets fs.FS // dist/assets, nil without pages
	public fs.FS // dist/public, nil without a public directory
	render func(context.Context, ssr.Input) (string, error)
	pool   *ssr.Pool // runs render, nil without an SSR bundle or in tests
	// pages is set when there are pages to render. Without it pages answer JSON.
	pages bool
	// preload holds, by the path of a page, the module preloads its document carries.
	preload map[string]pagePreload
}

// readFront reads the front end in dist: dist/build.json names the build and its assets,
// dist/assets and dist/public are served as files. render runs the SSR bundle, nil when the
// application has no pages.
func (s *Server) readFront(dist fs.FS, render func(context.Context, ssr.Input) (string, error)) (*front, error) {
	data, err := fs.ReadFile(dist, "build.json")
	if err != nil {
		return nil, fmt.Errorf("tifa: read the build: %w", err)
	}
	f := &front{render: render}
	if err := json.Unmarshal(data, &f.build); err != nil {
		return nil, fmt.Errorf("tifa: build.json: %w", err)
	}
	if f.build.ID == "" {
		return nil, errors.New("tifa: build.json has no build ID")
	}
	if render != nil {
		source, err := fs.ReadFile(dist, "ssr.js")
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(source)
		f.build.bundle = hex.EncodeToString(sum[:16])
	}
	for _, dir := range []struct {
		name string
		fsys *fs.FS
	}{{"assets", &f.assets}, {"public", &f.public}} {
		if _, err := fs.Stat(dist, dir.name); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		sub, err := fs.Sub(dist, dir.name)
		if err != nil {
			return nil, err
		}
		*dir.fsys = sub
	}
	if len(s.pages) > 0 && (render == nil || f.build.Head == "") {
		return nil, errors.New("tifa: the application has pages but the build has no bundles")
	}
	// Without pages there is nothing to render, and unmatched URLs keep their JSON answer.
	f.pages = len(s.pages) > 0
	f.preload = preloads(s.modules, f.build.Preload)
	return f, nil
}

// pagePreload is what preloads a page's chunks: the IDs of its Layouts and route, outermost
// first, and links[k], the module preloads of the first k of them.
type pagePreload struct {
	ids   []string
	links []string
}

// preloads writes, for each page, a module preload of every file its Layouts and route need
// beyond the head of the build: the files build.json lists for each, outermost first, each file
// once. The links come right after the head, before the tags of the route head. The browser then
// fetches the chunks of the page alongside the entry script instead of one after another as the
// imports turn up, and the page hydrates as soon as with all of them in the entry. A URL that
// matches no page shows the root Layout, which is part of the entry, and preloads nothing. tifa
// writes no nonce on these links, as on the other tags of the head.
func preloads(modules map[string][]string, files map[string][]string) map[string]pagePreload {
	out := make(map[string]pagePreload, len(modules))
	for path, ids := range modules {
		var b strings.Builder
		seen := map[string]bool{}
		p := pagePreload{ids: ids, links: []string{""}}
		for _, id := range ids {
			for _, file := range files[id] {
				if !seen[file] {
					seen[file] = true
					fmt.Fprintf(&b, `<link rel="modulepreload" href="%s">`, html.EscapeString(file))
				}
			}
			p.links = append(p.links, b.String())
		}
		out[path] = p
	}
	return out
}

// preloadOf is the module preloads of the page at path with the results of its Loads. The
// router shows the page down to the outermost level whose Load failed. A Layout's 404 shows the
// NotFound of the level above, so the Layout's chunk is left out. Any other failure, and a 404
// of the page, shows the Error or the NotFound of that level or of one above, so the chunk of
// that level is kept.
func (f *front) preloadOf(path string, items map[string]routeResult) string {
	p, ok := f.preload[path]
	if !ok {
		return ""
	}
	for i, id := range p.ids {
		r, ok := items[id]
		if !ok || r.Status == http.StatusOK {
			continue
		}
		if r.Status == http.StatusNotFound && i < len(p.ids)-1 {
			return p.links[i]
		}
		return p.links[i+1]
	}
	return p.links[len(p.ids)]
}

// setFront serves the front end in dist with render, see readFront.
func (s *Server) setFront(dist fs.FS, render func(context.Context, ssr.Input) (string, error)) error {
	f, err := s.readFront(dist, render)
	if err != nil {
		return err
	}
	return s.swapFront(f)
}

// openFront serves the front end in dist, the dist/ directory tifa build writes. With an SSR
// bundle (ssr.js) it starts the VM pool that renders pages. Calling it again replaces the front
// end it served before, which tifa dev does after it rebuilt the SSR bundle (see DevMain).
// When it fails, the front end served before stays. Under tifa dev (s.dev set) the pool logs
// the Intl calls that leave the locale or the time zone to the defaults.
func (s *Server) openFront(dist fs.FS, cfg config.SSR) error {
	source, err := fs.ReadFile(dist, "ssr.js")
	if errors.Is(err, fs.ErrNotExist) {
		return s.setFront(dist, nil)
	}
	if err != nil {
		return err
	}
	sourceMap, err := fs.ReadFile(dist, "ssr.js.map")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	pool, err := ssr.NewPool(string(source), sourceMap, cfg, s.dev != nil)
	if err != nil {
		return err
	}
	f, err := s.readFront(dist, pool.Render)
	if err != nil {
		return errors.Join(err, pool.Close())
	}
	f.pool = pool
	return s.swapFront(f)
}

// swapFront serves f from now on and closes the VM pool of the front end it replaces. It waits
// for the renders that run on that pool, so no render uses a closed pool.
func (s *Server) swapFront(f *front) error {
	s.rendering.Lock()
	old := s.front.Load()
	s.front.Store(f)
	s.rendering.Unlock()
	return old.close()
}

// closeFront closes the VM pool of the front end, after the server stopped.
func (s *Server) closeFront() error {
	return s.front.Load().close()
}

func (f *front) close() error {
	if f == nil || f.pool == nil {
		return nil
	}
	return f.pool.Close()
}

// pagesServed reports whether the server renders pages, see front.pages.
func (s *Server) pagesServed() bool {
	f := s.front.Load()
	return f != nil && f.pages
}

// serveStatic answers GET and HEAD for a file of the browser bundle (/assets/...) or of the
// public directory, before routing and without OnRequest. It reports whether it answered.
// Bundle files carry a content hash in their names and are cached for good. A name under
// /assets/ that the bundle lacks may still be a public file, such as public/assets/logo.svg.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" && r.Method != "HEAD" {
		return false
	}
	f := s.front.Load()
	if f == nil {
		return false
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if rest, ok := strings.CutPrefix(name, "assets/"); ok && serveFile(w, r, f.assets, rest, "public, max-age=31536000, immutable", "", s.cfg.Compress) {
		return true
	}
	// The embedded files have no modification time, so the ETag from the content hash of the
	// build is the only validator that lets a revalidation end in a 304.
	etag := ""
	if hash, ok := f.build.Public[name]; ok {
		etag = `"` + hash + `"`
	}
	return serveFile(w, r, f.public, name, "no-cache", etag, false)
}

// serveFile answers with the regular file name of fsys and reports whether it exists. With
// gz, a GET that accepts gzip gets the file name.gz that tifa build compressed, when there is
// one, as nginx's gzip_static and Caddy's precompressed do: with the type of name, and a range
// of a Range request taken from the compressed file. HEAD is answered with name, as gzhttp
// answers HEAD uncompressed. A non-empty etag is sent as the ETag, and a request with a matching
// If-None-Match gets 304. Without one the validator is the modification time of the file, which
// a file of the directory tifa dev serves has and an embedded file does not.
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name, cache, etag string, gz bool) bool {
	if fsys == nil || name == "" || !fs.ValidPath(name) {
		return false
	}
	if info, err := fs.Stat(fsys, name); err != nil || !info.Mode().IsRegular() {
		return false
	}
	h := w.Header()
	h.Set("Cache-Control", cache)
	if etag != "" {
		h.Set("ETag", etag)
	}
	if gz && r.Method == "GET" && quality(r.Header.Get("Accept-Encoding"), "gzip") > 0 {
		info, err := fs.Stat(fsys, name+".gz")
		if contentType := mime.TypeByExtension(path.Ext(name)); err == nil && info.Mode().IsRegular() && contentType != "" {
			h.Set("Content-Type", contentType)
			h.Set("Content-Encoding", "gzip")
			// ServeContent leaves the length out of an answer with a Content-Encoding.
			if r.Header.Get("Range") == "" {
				h.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
			}
			name += ".gz"
		}
	}
	http.ServeFileFS(w, r, fsys, name)
	return true
}

// routeResult is the outcome of one Load as the browser reads it (RouteResult in data.ts).
type routeResult struct {
	Status int `json:"status"`
	Data   any `json:"data,omitempty"`
	// JSON holds Data as the JSON text Go writes, in place of Data, in the results Go puts into
	// a page (InjectedResult in data.ts), see injectedResults.
	JSON     string `json:"json,omitempty"`
	Error    string `json:"error,omitempty"`
	Location string `json:"location,omitempty"`
}

// loaderText writes the data of a Load as the JSON text a page carries. The render parses that
// text, and the page carries it to the browser as it is, so the server and the browser read the
// same values. A JSON writer in the VM would write -0 as 0, and a page that formats it with Intl
// would then hydrate other text than the server rendered. A character above U+FFFF goes into the
// text as the escapes of its surrogate pair, see ssr.EscapeAboveFFFF.
func loaderText(data any) (string, error) {
	text, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(ssr.EscapeAboveFFFF(text)), nil
}

// injectedResults gives each successful result its data as the JSON text Go writes (loaderText),
// in a page that skips SSR, which carries the results to the browser (Boot in data.ts).
func injectedResults(items map[string]routeResult) (map[string]routeResult, error) {
	out := make(map[string]routeResult, len(items))
	for id, item := range items {
		if item.Status == http.StatusOK {
			text, err := loaderText(item.Data)
			if err != nil {
				return nil, err
			}
			item.Data, item.JSON = nil, text
		}
		out[id] = item
	}
	return out, nil
}

// renderResult is a routeResult as the VM gets it in a render (RenderResult in server.tsx, not the
// InjectedResult in data.ts that a page without SSR carries). The data of a successful Load is not
// in it: its JSON text is the argument of startRender that Text numbers, counting from the first
// text.
type renderResult struct {
	Status   int    `json:"status"`
	Text     *int   `json:"text,omitempty"`
	Error    string `json:"error,omitempty"`
	Location string `json:"location,omitempty"`
}

// renderInput splits the results of the Loads for a render into the JSON of what is not data, the
// statuses, errors and redirects, and the JSON text of the data of each successful one (loaderText),
// which goes to the VM as it is, apart from the JSON, as an argument of its own. Neither Go has
// to escape each text into the outer JSON nor the VM to parse it out of there. The texts follow
// the order of the route IDs, so the same results always give the same input.
func renderInput(items map[string]routeResult) (data []byte, texts []string, err error) {
	results := make(map[string]renderResult, len(items))
	for _, id := range slices.Sorted(maps.Keys(items)) {
		item := items[id]
		result := renderResult{Status: item.Status, Error: item.Error, Location: item.Location}
		if item.Status == http.StatusOK {
			text, err := loaderText(item.Data)
			if err != nil {
				return nil, nil, err
			}
			number := len(texts)
			result.Text = &number
			texts = append(texts, text)
		}
		results[id] = result
	}
	data, err = json.Marshal(results)
	return data, texts, err
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
		if !s.pagesServed() {
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
	if c.Header(buildHeader) != s.front.Load().build.ID {
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
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	_, err = w.Write(data)
	return err
}

// dataRedirect is how a data request answers a redirect: 200 and this JSON, which the
// collector turns into a router redirect. A 3xx would be followed by fetch, still asking for
// the routes of the old page. A handler call of the browser gets the same JSON, marked with
// redirectHeader, since the result of a handler may look like it.
type dataRedirect struct {
	Redirect string `json:"redirect"`
	Status   int    `json:"status"`
}

// renderPage renders the document in the VM. A successful page gets an ETag from the hash of
// the build ID, the SSR bundle, the URL and the data, which are all the render depends on. A matching
// If-None-Match is answered 304, and with cache the HTML of an earlier render is reused, or of the
// render that other requests for the same page wait for at the time (renderCached). With
// server.compress the ETag is weak: the page goes out in several encodings under it, and the 304,
// which is answered before the encoding is known, carries the same ETag as the 200.
func (s *Server) renderPage(c *tifa.Ctx, items map[string]routeResult, status int, cache bool) error {
	r := c.Request()
	data, texts, err := renderInput(items)
	if err != nil {
		return err
	}
	// The render runs on the pool of f, which swapFront closes only after this returns. Only
	// tifa dev swaps the front end, so in production the lock is never contended.
	s.rendering.RLock()
	defer s.rendering.RUnlock()
	f := s.front.Load()
	// The preloads follow from the build, the matched path, which the URL picks, and the results
	// of the Loads, so the ETag and the HTML cache need not cover more.
	in := ssr.Input{BuildID: f.build.ID, URL: r.URL.RequestURI(), Data: data, Texts: texts, Preload: f.preloadOf(c.Pattern(), items)}
	var key string
	if status == http.StatusOK {
		sum := sha256.New()
		io.WriteString(sum, in.BuildID+"\x00"+f.build.bundle+"\x00"+in.URL+"\x00"+string(data))
		for _, text := range texts {
			io.WriteString(sum, "\x00"+text)
		}
		key = `"` + hex.EncodeToString(sum.Sum(nil)[:16]) + `"`
		etag := key
		if s.cfg.Compress {
			etag = "W/" + key
		}
		c.SetHeader("ETag", etag)
		if etagMatches(c.Header("If-None-Match"), key) {
			c.SetHeader(renderHeader, "none")
			return c.NoContent(http.StatusNotModified)
		}
	}
	if cache && key != "" {
		return s.renderCached(c, f, in, key, status)
	}
	page, err := f.render(r.Context(), in)
	if err != nil {
		return renderFailed(c, err)
	}
	c.SetHeader(renderHeader, "vm")
	return writeHTML(c, status, page)
}

// errClientGone is what Next returns for a request whose client went away while it waited for
// the render of a Cache = true page. Server.fail cuts the connection of such a request, without an
// answer, without OnError and without a log line.
var errClientGone = fmt.Errorf("tifa: the client went away: %w", context.Canceled)

// renderCached answers a page of a Cache = true route from the HTML cache, or from the render
// that every request missing the same key at the time waits for (htmlCache.fetch). Each request
// writes the page with its own headers and in its own encoding, as a cache hit does. With
// server.compress the render compresses the page into both its forms once, so no request
// compresses it again, whatever encoding the request that started the render takes.
func (s *Server) renderCached(c *tifa.Ctx, f *front, in ssr.Input, key string, status int) error {
	method, path := c.Request().Method, c.Request().URL.Path
	// A render that fails once no request waits has no OnError to go to, so it is logged.
	unheard := func(err error) {
		if !errors.As(err, new(*ssr.RedirectError)) {
			log.Printf("tifa: %s %s: %v, and no request waits for the render", method, path, &renderError{err: err})
		}
	}
	got, err := s.cache.fetch(c.Request().Context(), key, unheard, func(ctx context.Context) (cachedPage, bool, error) {
		html, err := f.render(ctx, in)
		if err != nil || !s.cache.holds(html) {
			return cachedPage{html: html}, false, err
		}
		page := cachedPage{html: html}
		if s.cfg.Compress {
			page.gzip, page.zstd = compressPage(html)
		}
		return page, true, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) && errors.Is(c.Request().Context().Err(), context.Canceled) {
			return errClientGone
		}
		return renderFailed(c, err)
	}
	c.SetHeader(renderHeader, got.how)
	if !got.held {
		return writeHTML(c, status, got.page.html)
	}
	return writeCached(c, status, got.page, s.formEncoding(c))
}

// renderFailed answers a render that returned err: a redirect of the router, or a *renderError
// for the error flow.
func renderFailed(c *tifa.Ctx, err error) error {
	var redirect *ssr.RedirectError
	if errors.As(err, &redirect) {
		// The router spells the URL differently, /%61 for /a, and sends the browser there.
		// The redirect carries no ETag. The render is a function of the build, the URL and the
		// data, so an ETag a client got with a 200 for this URL never belongs to a redirect,
		// and 304 is only answered to a client that made its If-None-Match up.
		c.Writer().Header().Del("ETag")
		http.Redirect(c.Writer(), c.Request(), redirect.Location, redirect.Status)
		return nil
	}
	return &renderError{err: err}
}

// formEncoding is the encoding a page of the HTML cache goes out in to c: the one compressed would
// pick, and none with server.compress off or No-Gzip-Compression set.
func (s *Server) formEncoding(c *tifa.Ctx) string {
	if !s.cfg.Compress || len(c.Writer().Header()[gzhttp.HeaderNoCompression]) > 0 {
		return ""
	}
	return pageEncoding(c.Request())
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
	f := s.front.Load()
	injected, err := injectedResults(items)
	if err != nil {
		return err
	}
	boot, err := json.Marshal(map[string]any{
		"url": c.Request().URL.RequestURI(), "buildId": f.build.ID, "data": injected,
	})
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	b.WriteString(f.build.Head)
	b.WriteString(f.preloadOf(c.Pattern(), items))
	b.WriteString(`</head><body><div id="tifa-root"></div>`)
	// json.Marshal escapes <, >, &, U+2028 and U+2029, so the data cannot close the script element.
	fmt.Fprintf(&b, `<script type="application/json" id="tifa-data">%s</script></body></html>`, boot)
	c.SetHeader(renderHeader, "none")
	return writeHTML(c, status, b.String())
}

// writeCached writes a page of the HTML cache in its form for encoding (formEncoding), and as
// HTML when it has none. compressed leaves an answer that has a Content-Encoding as it is.
func writeCached(c *tifa.Ctx, status int, page cachedPage, encoding string) error {
	var body []byte
	switch encoding {
	case "gzip":
		body = page.gzip
	case "zstd":
		body = page.zstd
	}
	if body == nil {
		return writeHTML(c, status, page.html)
	}
	w := c.Writer()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Encoding", encoding)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, err := w.Write(body)
	return err
}

func writeHTML(c *tifa.Ctx, status int, page string) error {
	w := c.Writer()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(page)))
	w.WriteHeader(status)
	_, err := io.WriteString(w, page)
	return err
}

// errorPage answers a failed page request with a page and the error's status. The document
// survives any failure, so the router shows the right level's NotFound or Error component:
//   - a Load of the chain failed: the page with the results of the whole chain (items)
//   - no route matched: the root Layout's NotFound, which needs no data
//
// Anything else, a failed OnRequest for one, gets a plain page from Go (see fallback). So does
// a page whose render fails, after OnError has seen that failure. The plain page is not
// rendered, so a failing error page ends there. err is the error that e stands for.
func (s *Server) errorPage(c *tifa.Ctx, e tifa.Error, err error, items map[string]routeResult, ssrOn bool) {
	if items == nil {
		if c.Pattern() != "" || e.Status != http.StatusNotFound {
			s.fallback(c, e, err)
			return
		}
		items, ssrOn = map[string]routeResult{}, s.rootSSR
	}
	var renderErr error
	if ssrOn {
		renderErr = s.renderPage(c, items, e.Status, false)
	} else {
		renderErr = s.dataOnly(c, items, e.Status)
	}
	if renderErr != nil {
		renderErr = fmt.Errorf("render the %d page: %w", e.Status, renderErr)
		_, public := s.classify(c, renderErr)
		s.fallback(c, public, errors.Join(err, renderErr))
	}
}

// fallback answers with the plain page of e. Under tifa dev the page also names the request
// and shows err, the error behind e, and it loads the dev client, which reloads the page once
// a rebuild serves a fix. A built app never shows err.
func (s *Server) fallback(c *tifa.Ctx, e tifa.Error, err error) {
	if s.dev == nil {
		writeFallback(c.Writer(), e)
		return
	}
	r := c.Request()
	what := "failed."
	if errors.As(err, new(*renderError)) {
		what = "failed while rendering on the server."
	}
	title := html.EscapeString(fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status)))
	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>%s</title></head><body><h1>%s</h1>`, title, title)
	fmt.Fprintf(&b, "<p>%s %s %s</p>", html.EscapeString(r.Method), html.EscapeString(r.URL.RequestURI()), what)
	if e.Message != http.StatusText(e.Status) {
		fmt.Fprintf(&b, "<p>Users see: %s</p>", html.EscapeString(e.Message))
	}
	// Before the error, whose stack trace can be long.
	b.WriteString("<p>This page reloads after you save a fix. The built app shows only the status and the public message.</p>")
	fmt.Fprintf(&b, `<pre style="white-space: pre-wrap">%s</pre>`, html.EscapeString(err.Error()))
	fmt.Fprintf(&b, `<script>window.__tifaDevErrorPage = true</script><script src="%s"></script></body></html>`, clientPath)
	w := c.Writer()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(b.Len()))
	w.WriteHeader(e.Status)
	_, _ = io.WriteString(w, b.String())
}

// writeFallback writes a plain error page when the application cannot render one.
func writeFallback(w http.ResponseWriter, e tifa.Error) {
	title := html.EscapeString(fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status)))
	page := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>`,
		title, title, html.EscapeString(e.Message))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(page)))
	w.WriteHeader(e.Status)
	_, _ = io.WriteString(w, page)
}
