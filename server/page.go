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
	"time"

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
	// preload holds, by the path of a page or the ID of a Layout (see Server.modules), the module
	// preloads its document carries.
	preload map[string]pagePreload
	// timeout is ssr.timeout, what the renders of one request may take together, 0 for no limit.
	timeout time.Duration
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
// first, and links[k], the module preloads of the first k of them. page is false for the chain of
// a Layout that a URL matching no page shows its 404 inside.
type pagePreload struct {
	ids   []string
	links []string
	page  bool
}

// preloads writes, for each page, a module preload of every file its Layouts and route need
// beyond the head of the build: the files build.json lists for each, outermost first, each file
// once. The links come right after the head, before the tags of the route head. The browser then
// fetches the chunks of the page alongside the entry script instead of one after another as the
// imports turn up, and the page hydrates as soon as with all of them in the entry. A URL that
// matches no page preloads the Layouts it shows its 404 inside. tifa writes no nonce on these
// links, as on the other tags of the head.
func preloads(modules map[string][]string, files map[string][]string) map[string]pagePreload {
	out := make(map[string]pagePreload, len(modules))
	for path, ids := range modules {
		var b strings.Builder
		seen := map[string]bool{}
		p := pagePreload{ids: ids, links: []string{""}, page: strings.HasPrefix(path, "/")}
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

// preloadOf is the module preloads of the page at key (see front.preload) with the results of
// its Loads. The router shows the page down to the outermost level whose Load failed. A failed
// Layout shows the Error of the level above, so the Layout's chunk is left out. A failed page shows
// the first Error from the page up, which may be the page's own, so the page's chunk is kept.
func (f *front) preloadOf(key string, items map[string]routeResult) string {
	p, ok := f.preload[key]
	if !ok {
		return ""
	}
	for i, id := range p.ids {
		r, ok := items[id]
		if !ok || r.Status == http.StatusOK {
			continue
		}
		if p.page && i == len(p.ids)-1 {
			return p.links[i+1]
		}
		return p.links[i]
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
	f.pool, f.timeout = pool, cfg.Timeout
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
	key    string // the key of the page's preloads, see front.preload
}

func (f *loadFailure) Error() string { return f.errs[f.failed].Error() }
func (f *loadFailure) Unwrap() error { return f.errs[f.failed] }

// renderError is a failed SSR render of in on the pool of f, which started at start. Server.rescue
// renders the page again with the error shown at a level of the route tree, and when that fails
// too the error page comes from Go, not from the VM.
type renderError struct {
	err   error
	f     *front
	in    ssr.Input
	start time.Time
	key   string
}

func (e *renderError) Error() string { return "tifa: render page: " + e.err.Error() }
func (e *renderError) Unwrap() error { return e.err }

// runChain runs the Loads of chain and returns their results by route ID, or a *loadFailure
// when one failed. key is the key of the page's preloads.
func runChain(c *tifa.Ctx, chain []load, ssr bool, key string) (map[string]routeResult, error) {
	values, errs := runLoads(c, chain)
	for i, err := range errs {
		if err != nil {
			return nil, &loadFailure{chain: chain, values: values, errs: errs, failed: i, data: c.IsData(), ssr: ssr, key: key}
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
			return s.dataRequest(c, chain, r.Path)
		}
		items, err := runChain(c, chain, r.SSR, r.Path)
		switch {
		case err != nil:
			return err
		case !r.SSR:
			return s.dataOnly(c, items, http.StatusOK, r.Path)
		}
		return s.renderPage(c, items, http.StatusOK, r.Cache, r.Path)
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
func (s *Server) dataRequest(c *tifa.Ctx, chain []load, key string) error {
	selected, err := s.listedLoads(c, chain)
	if err != nil {
		return err
	}
	items, err := runChain(c, selected, false, key)
	if err != nil {
		return err
	}
	return writeData(c.Writer(), http.StatusOK, items)
}

// listedLoads returns the Loads of chain that a data request lists in X-Tifa-Route, each once.
func (s *Server) listedLoads(c *tifa.Ctx, chain []load) ([]load, error) {
	if c.Header(buildHeader) != s.front.Load().build.ID {
		return nil, tifa.Error{Status: http.StatusConflict, Message: "the application was updated, reload the page"}
	}
	var selected []load
	for _, id := range strings.Split(c.Header(routeHeader), ",") {
		id = strings.TrimSpace(id)
		i := slices.IndexFunc(chain, func(l load) bool { return l.id == id })
		if i < 0 {
			return nil, tifa.Error{Status: http.StatusBadRequest, Message: fmt.Sprintf("route %q has no data at this URL", id)}
		}
		if !slices.ContainsFunc(selected, func(l load) bool { return l.id == id }) {
			selected = append(selected, chain[i])
		}
	}
	return selected, nil
}

// writeData writes the answer of a data request. A Cache-Control of OnRequest or a Load, such
// as private, no-store, wins over the default of the writer (defaultCacheControl). The Loads have
// returned, so the header map is no longer shared.
func writeData(w http.ResponseWriter, status int, value any) error {
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
// which is answered before the encoding is known, carries the same ETag as the 200. key is the key
// of the page's preloads (see front.preload).
func (s *Server) renderPage(c *tifa.Ctx, items map[string]routeResult, status int, cache bool, key string) error {
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
	in := ssr.Input{BuildID: f.build.ID, URL: r.URL.RequestURI(), Data: data, Texts: texts, Preload: f.preloadOf(key, items)}
	failed := &renderError{f: f, in: in, key: key}
	var tag string
	if status == http.StatusOK {
		sum := sha256.New()
		io.WriteString(sum, in.BuildID+"\x00"+f.build.bundle+"\x00"+in.URL+"\x00"+string(data))
		for _, text := range texts {
			io.WriteString(sum, "\x00"+text)
		}
		tag = `"` + hex.EncodeToString(sum.Sum(nil)[:16]) + `"`
		etag := tag
		if s.cfg.Compress {
			etag = "W/" + tag
		}
		c.SetHeader("ETag", etag)
		if etagMatches(c.Header("If-None-Match"), tag) {
			c.SetHeader(renderHeader, "none")
			return c.NoContent(http.StatusNotModified)
		}
	}
	failed.start = time.Now()
	if cache && tag != "" {
		return s.renderCached(c, f, in, tag, status, failed)
	}
	page, err := f.render(r.Context(), in)
	if err != nil {
		return renderFailed(c, err, failed)
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
func (s *Server) renderCached(c *tifa.Ctx, f *front, in ssr.Input, key string, status int, failed *renderError) error {
	method, path := c.Request().Method, c.Request().URL.Path
	// A render that fails once no request waits has no OnError to go to, so it is logged.
	unheard := func(err error) {
		if !errors.As(err, new(*ssr.RedirectError)) {
			log.Printf("tifa: %s %s: %v, and no request waits for the render", method, path, &renderError{err: err, f: f, in: in})
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
		return renderFailed(c, err, failed)
	}
	c.SetHeader(renderHeader, got.how)
	if !got.held {
		return writeHTML(c, status, got.page.html)
	}
	return writeCached(c, status, got.page, s.formEncoding(c))
}

// renderFailed answers a render that returned err: a redirect of the router, or failed with err
// for the error flow.
func renderFailed(c *tifa.Ctx, err error, failed *renderError) error {
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
	failed.err = err
	return failed
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
func (s *Server) dataOnly(c *tifa.Ctx, items map[string]routeResult, status int, key string) error {
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
	b.WriteString(f.preloadOf(key, items))
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
// survives any failure, so the router shows the Error of the right level:
//   - a Load of the chain failed: the page with the results of the whole chain (items)
//   - no page matched: the 404 inside the Layouts the URL shows it inside, with their results
//
// key is the key of the page's preloads. When OnRequest answers a URL that matches no page with
// tifa.ErrNotFound itself, no Load ran, and every Load of those Layouts counts as a 404. Anything
// else, a failed OnRequest for one, gets a plain page from Go (see fallback). A page whose render
// fails, after OnError has seen that failure, renders again with that error (see rescue). err is
// the error that e stands for.
func (s *Server) errorPage(c *tifa.Ctx, e tifa.Error, err error, items map[string]routeResult, ssrOn bool, key string) {
	if items == nil {
		if c.Pattern() != "" || e.Status != http.StatusNotFound {
			s.fallback(c, e, err)
			return
		}
		key = s.notFoundAt(c.Request())
		at := s.layouts[key]
		items, ssrOn = make(map[string]routeResult, len(at.chain)), at.ssr
		for _, l := range at.chain {
			items[l.id] = routeResult{Status: e.Status, Error: e.Message}
		}
	}
	var renderErr error
	if ssrOn {
		renderErr = s.renderPage(c, items, e.Status, false, key)
	} else {
		renderErr = s.dataOnly(c, items, e.Status, key)
	}
	if renderErr != nil {
		renderErr = fmt.Errorf("render the %d page: %w", e.Status, renderErr)
		_, public := s.classify(c, renderErr)
		var failed *renderError
		if errors.As(renderErr, &failed) {
			s.rescue(c, public, errors.Join(err, renderErr), failed)
			return
		}
		s.fallback(c, public, errors.Join(err, renderErr))
	}
}

// rescue answers a page whose render failed with err, which e stands for: it renders the page
// again with e shown at the first level of its route tree that can show it, from the level that
// failed (the page, or the Layout of a URL that matches no page) up to the root, each try on a
// fresh VM, since a failed render retires its VM. When every level fails, or failed.err is not an
// error of the page's code or of the VM's memory, the page comes from Go (see fallback). The tries
// share the time budget of the first render, ssr.timeout, so a render that ran out of it is not
// tried again. The answer is never cached.
func (s *Server) rescue(c *tifa.Ctx, e tifa.Error, err error, failed *renderError) {
	c.Writer().Header().Del("ETag")
	levels, ok := ssr.Levels(failed.err)
	if !ok && errors.Is(failed.err, ssr.ErrOutOfMemory) {
		// The memory ran out before the bundle could say: the matches are at most the root, the
		// Layouts and the route.
		levels, ok = len(failed.f.preload[failed.key].ids)+2, true
	}
	if !ok || retriedNot(failed.err) {
		s.fallback(c, e, err)
		return
	}
	// The pool of the failed render serves the tries, as long as it serves pages.
	s.rendering.RLock()
	defer s.rendering.RUnlock()
	if s.front.Load() != failed.f {
		s.fallback(c, e, err)
		return
	}
	var deadline time.Time
	if failed.f.timeout > 0 {
		deadline = failed.start.Add(failed.f.timeout)
	}
	var errs []error
	for level := 0; level < levels; level++ {
		in := failed.in
		in.Error, in.Deadline = &ssr.PlacedError{Status: e.Status, Message: e.Message, Level: level}, deadline
		page, tryErr := failed.f.render(c.Request().Context(), in)
		if tryErr == nil {
			if s.dev != nil {
				page = s.devPageError(c, e, err, page)
			}
			c.SetHeader("Cache-Control", "no-store")
			c.SetHeader(renderHeader, "vm")
			_ = writeHTML(c, e.Status, page)
			return
		}
		errs = append(errs, fmt.Errorf("show the error at level %d: %w", level, tryErr))
		if retriedNot(tryErr) {
			break
		}
		if n, ok := ssr.Levels(tryErr); ok {
			levels = min(levels, n)
		}
	}
	tries := errors.Join(errs...)
	log.Printf("tifa: %s %s: the page with the error failed too: %v", c.Request().Method, c.Request().URL.Path, tries)
	s.fallback(c, e, errors.Join(err, tries))
}

// retriedNot reports whether a render that failed with err is never tried again: it ran out of
// time, its request went away, the pool is full or closed, or the router redirects.
func retriedNot(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.Is(err, tifa.ErrSSRBusy) || errors.Is(err, ssr.ErrClosed) || errors.As(err, new(*ssr.RedirectError))
}

// devPageError adds to the page of a failed render under tifa dev what the plain page of fallback
// shows, in the overlay of the dev client, and marks it as an error page, which reloads after a
// rebuild serves a fix.
func (s *Server) devPageError(c *tifa.Ctx, e tifa.Error, err error, page string) string {
	r := c.Request()
	text := fmt.Sprintf("%s %s failed while rendering on the server.\n", r.Method, r.URL.RequestURI())
	if e.Message != http.StatusText(e.Status) {
		text += "Users see: " + e.Message + "\n"
	}
	text += "The page shows the Error component for it. It reloads after you save a fix. The built app shows only the status and the public message.\n\n" + err.Error()
	// json.Marshal escapes <, >, &, U+2028 and U+2029, so the text cannot close the script element.
	shown, _ := json.Marshal(map[string]string{"title": fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status)), "text": text})
	script := fmt.Sprintf(`<script>window.__tifaDevErrorPage = true; window.__tifaDevPageError = %s</script><script src="%s"></script>`, shown, clientPath)
	i := strings.LastIndex(page, "</body>")
	if i < 0 {
		return page + script
	}
	return page[:i] + script + page[i:]
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
	noStoreUnlessSet(w.Header())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(page)))
	w.WriteHeader(e.Status)
	_, _ = io.WriteString(w, page)
}
