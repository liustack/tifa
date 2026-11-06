package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"maps"
	"mime"
	"net/http"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/klauspost/compress/gzhttp"
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/core"
	"github.com/liustack/tifa/internal/deverrors"
	"github.com/liustack/tifa/internal/ssr"
)

// Headers of the page protocol, see js/runtime/data.ts. A navigation data request has no header
// of its own: it is the URL of the page with the query parameter core.DataParam.
const (
	buildHeader  = "X-Tifa-Build"  // the build a call comes from, and the build a 409 names
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
	render func(context.Context, ssr.Input) (ssr.Page, error)
	pool   *ssr.Pool // runs render, nil without an SSR bundle or in tests
	// headScripts and headStyles are the hashes of the inline scripts and styles of build.Head,
	// which every document carries (see renderedPolicy).
	headScripts, headStyles []string
	// shared is set for the pool of a test server, which the servers of a test binary share: the
	// front end does not close it.
	shared bool
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
func (s *server) readFront(dist fs.FS, render func(context.Context, ssr.Input) (ssr.Page, error)) (*front, error) {
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
	f.headScripts, f.headStyles = ssr.InlineHashes(f.build.Head)
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
func (s *server) setFront(dist fs.FS, render func(context.Context, ssr.Input) (ssr.Page, error)) error {
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
func (s *server) openFront(dist fs.FS, cfg config.SSR) error {
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
	f, err := s.readFront(dist, pool.RenderPage)
	if err != nil {
		return errors.Join(err, pool.Close())
	}
	f.pool, f.timeout = pool, cfg.Timeout
	return s.swapFront(f)
}

// swapFront serves f from now on and closes the VM pool of the front end it replaces. It waits
// for the renders that run on that pool, so no render uses a closed pool.
func (s *server) swapFront(f *front) error {
	s.rendering.Lock()
	old := s.front.Load()
	s.front.Store(f)
	s.rendering.Unlock()
	return old.close()
}

// closeFront closes the VM pool of the front end, after the server stopped.
func (s *server) closeFront() error {
	return s.front.Load().close()
}

func (f *front) close() error {
	if f == nil || f.pool == nil || f.shared {
		return nil
	}
	return f.pool.Close()
}

// pagesServed reports whether the server renders pages, see front.pages.
func (s *server) pagesServed() bool {
	f := s.front.Load()
	return f != nil && f.pages
}

// serveStatic answers GET and HEAD for a file of the browser bundle (/assets/...) or of the
// public directory, before routing and without OnRequest. It reports whether it answered.
// Bundle files carry a content hash in their names and are cached for good. A name under
// /assets/ that the bundle lacks may still be a public file, such as public/assets/logo.svg.
func (s *server) serveStatic(w http.ResponseWriter, r *http.Request) bool {
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
	Data   any `json:"data,omitzero"`
	// JSON holds Data as the JSON text Go writes, in place of Data, in the results Go puts into
	// a page (InjectedResult in data.ts), see injectedResults.
	JSON     string `json:"json,omitempty"`
	Error    string `json:"error,omitempty"`
	Location string `json:"location,omitempty"`
	// html is the html of the load of the result: its data may hold tifa.HTML values, which a page
	// the server renders carries as markers (pageText).
	html bool
}

// loaderText writes the data of a Load as the JSON text a page carries. The render parses that
// text, and the page carries it to the browser as it is, so the server and the browser read the
// same values. A JSON writer in the VM would write -0 as 0, and a page that formats it with Intl
// would then hydrate other text than the server rendered. A character above U+FFFF goes into the
// text as the escapes of its surrogate pair, see ssr.EscapeAboveFFFF.
func loaderText(data any) (string, error) {
	text, err := core.MarshalJSON(data)
	if err != nil {
		return "", err
	}
	return string(ssr.EscapeAboveFFFF(text)), nil
}

// htmlJSON writes v as JSON that a script element can hold: like the JSON of the app
// (core.JSONWrite), with <, >, & and U+2028 and U+2029 written as escapes, so that no </script or
// <!-- can end the element.
func htmlJSON(v any) ([]byte, error) {
	return json.Marshal(v, core.JSONWrite, jsontext.EscapeForHTML(true), jsontext.EscapeForJS(true))
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
	Status int  `json:"status"`
	Text   *int `json:"text,omitzero"`
	// Page is true when the page carries another text than the render reads, with markers in
	// place of tifa.HTML values (pageText). That text always goes into the page as a slot.
	Page     bool   `json:"page,omitzero"`
	Error    string `json:"error,omitempty"`
	Location string `json:"location,omitempty"`
}

// renderInput splits the results of the Loads for a render into the JSON of what is not data, the
// statuses, errors and redirects, and the JSON text of the data of each successful one (loaderText),
// which goes to the VM as it is, apart from the JSON, as an argument of its own. Neither Go has
// to escape each text into the outer JSON nor the VM to parse it out of there. The texts follow
// the order of the route IDs, so the same results always give the same input. pageTexts are the
// texts the page carries in their place (ssr.Input.PageTexts), nil when they are the same: the data
// of a Load the build marked has a marker in place of each tifa.HTML value there.
func renderInput(items map[string]routeResult) (data []byte, texts, pageTexts []string, err error) {
	results := make(map[string]renderResult, len(items))
	for _, id := range slices.Sorted(maps.Keys(items)) {
		item := items[id]
		result := renderResult{Status: item.Status, Error: item.Error, Location: item.Location}
		if item.Status == http.StatusOK {
			text, err := loaderText(item.Data)
			if err != nil {
				return nil, nil, nil, err
			}
			number := len(texts)
			result.Text = &number
			texts = append(texts, text)
			page, differs := text, false
			if item.html {
				if page, differs, err = pageText(item.Data, text); err != nil {
					return nil, nil, nil, err
				}
			}
			if differs && pageTexts == nil {
				pageTexts = slices.Clone(texts[:number])
			}
			if pageTexts != nil {
				pageTexts = append(pageTexts, page)
			}
			result.Page = differs
		}
		results[id] = result
	}
	data, err = core.MarshalJSON(results)
	return data, texts, pageTexts, err
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
	key    string    // the key of the page's preloads, see front.preload
	loads  time.Time // when the Loads started, see runChain
}

func (f *loadFailure) Error() string { return f.errs[f.failed].Error() }
func (f *loadFailure) Unwrap() error { return f.errs[f.failed] }

// renderError is a failed SSR render of in on the pool of f, which started at start, of data whose
// Loads started at loads. Server.rescue renders the page again with the error shown at a level of
// the route tree, and when that fails too the error page comes from Go, not from the VM.
type renderError struct {
	err   error
	f     *front
	in    ssr.Input
	start time.Time
	loads time.Time
	key   string
}

func (e *renderError) Error() string { return "tifa: render page: " + e.err.Error() }
func (e *renderError) Unwrap() error { return e.err }

// runChain runs the Loads of chain and returns their results by route ID, or a *loadFailure
// when one failed. key is the key of the page's preloads. loads is when the first Load of the
// request started, a Load that OnRequest ran through tifa.Use included: the data of the page is no
// older than that (see setDataAge).
func runChain(c *tifa.Ctx, chain []load, ssr bool, key string) (items map[string]routeResult, loads time.Time, err error) {
	loads = core.StartLoads(c)
	values, errs := runLoads(c, chain)
	for i, err := range errs {
		if err != nil {
			return nil, loads, &loadFailure{chain: chain, values: values, errs: errs, failed: i, data: c.IsData(), ssr: ssr, key: key, loads: loads}
		}
	}
	items = make(map[string]routeResult, len(chain))
	for i, l := range chain {
		items[l.id] = routeResult{Status: http.StatusOK, Data: values[i], html: l.html}
	}
	return items, loads, nil
}

// results sorts the errors of a failure with classify and returns the results by route ID and
// the sorted outermost failure. Loads that share a failure through tifa.Use return the same
// error, which classify sees once, so OnError reports it once.
func (s *server) results(c *tifa.Ctx, f *loadFailure) (items map[string]routeResult, redirect *core.RedirectError, public tifa.Error) {
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
			items[l.id] = routeResult{Status: http.StatusOK, Data: f.values[i], html: l.html}
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
			items[l.id] = routeResult{Status: x.redirect.Status, Location: x.redirect.URL}
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
//   - with the _tifa parameter, the data of the routes it asks for as JSON
//   - with SSR on its chain, the HTML the VM renders, or 304, or the cached HTML (unless RenderCache = false)
//   - with SSR = false somewhere on its chain, a minimal document carrying the data
//
// When a Load fails it returns a *loadFailure, for OnRequest to see as the error of Next.
// Without a front end it answers the chain's data as JSON by route ID.
func (s *server) pageHandler(r Route, chain []load) func(*tifa.Ctx) error {
	return func(c *tifa.Ctx) error {
		if !s.pagesServed() {
			return loadsAsJSON(c, chain)
		}
		if ttl := s.pageCache[r.Path]; ttl > 0 && anonymityOf(c.Request()) != nil {
			return s.cachedPageRequest(c, r, chain, ttl)
		}
		if c.IsData() {
			return s.dataRequest(c, chain, r.Path)
		}
		items, loads, err := runChain(c, chain, r.SSR, r.Path)
		switch {
		case err != nil:
			return err
		case !r.SSR:
			return s.dataOnly(c, items, http.StatusOK, r.Path)
		}
		return s.renderPage(c, items, loads, http.StatusOK, !r.NoRenderCache, r.Path)
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

// dataRequest answers a client navigation: the results of the routes that _tifa asks for,
// each with its own status. A browser running another build gets 409 and reloads the page.
// When a listed Load fails, Next returns the failure as it does for the page, and the answer
// is written once OnRequest has returned (see Server.fail).
func (s *server) dataRequest(c *tifa.Ctx, chain []load, key string) error {
	selected, err := s.listedLoads(c, chain)
	if err != nil {
		return err
	}
	items, _, err := runChain(c, selected, false, key)
	if err != nil {
		return err
	}
	session, err := s.browserSession(c)
	if err != nil {
		return err
	}
	if session != nil {
		if err := checkSessionCache(c.Writer().Header()); err != nil {
			return err
		}
	}
	alternates, err := s.navigationAlternates(c)
	if err != nil {
		return err
	}
	return writeDataBody(c.Writer(), http.StatusOK, dataAnswer{Routes: items, Session: session, Alternates: alternates})
}

// buildChanged is the 409 for a request from a page of another build. It names the build the
// server runs in X-Tifa-Build, which tells it from a 409 of the app, and the browser reloads the
// page once for that build (js/runtime/skew.ts).
func (s *server) buildChanged(c *tifa.Ctx) error {
	c.SetHeader(buildHeader, s.front.Load().build.ID)
	return tifa.Error{Status: http.StatusConflict, Message: "the application was updated, reload the page"}
}

// listedLoads returns the Loads of chain that a data request asks for with the digits of _tifa,
// one for each Load of the chain. A different number of digits is a 400, since the page and the
// server then do not mean the same chain.
func (s *server) listedLoads(c *tifa.Ctx, chain []load) ([]load, error) {
	d := core.Data(c)
	if d.Valid && d.Build != s.front.Load().build.ID {
		return nil, s.buildChanged(c)
	}
	if !d.Valid {
		return nil, tifa.Error{Status: http.StatusBadRequest, Message: "the query parameter _tifa is not <build>.<digits>"}
	}
	if len(d.Layers) != len(chain) {
		return nil, tifa.Error{Status: http.StatusBadRequest, Message: fmt.Sprintf("_tifa has %d digits, the data of this URL has %d routes", len(d.Layers), len(chain))}
	}
	var selected []load
	for i, bit := range []byte(d.Layers) {
		if bit == '1' {
			selected = append(selected, chain[i])
		}
	}
	return selected, nil
}

// dataAnswer is the body of a data request: the results of the routes by route ID, the session
// of the request when the app has sessions, and the versions of the page in other languages when
// it has some (see alternatesOf).
type dataAnswer struct {
	Routes     map[string]routeResult `json:"routes"`
	Session    *core.BrowserSession   `json:"session,omitzero"`
	Alternates jsontext.Value         `json:"alternates,omitzero"`
}

// writeData writes the answer of a data request. A Cache-Control of OnRequest or a Load, such
// as private, no-store, wins over the default of the writer (defaultCacheControl). The Loads have
// returned, so the header map is no longer shared.
func (s *server) writeData(c *tifa.Ctx, status int, items map[string]routeResult) error {
	session := s.answerSession(c)
	if session != nil {
		s.sealSessionAnswer(c) // this answer is already an error flow
	}
	return writeDataBody(c.Writer(), status, dataAnswer{Routes: items, Session: session})
}

func writeDataBody(w http.ResponseWriter, status int, value any) error {
	data, err := core.MarshalJSON(value)
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
	Redirect string               `json:"redirect"`
	Status   int                  `json:"status"`
	Session  *core.BrowserSession `json:"session,omitzero"`
}

// renderKey is the quoted hash of everything the HTML of a page is made from, which is the ETag
// of the page and the key of the render cache. The HTML is a function of ssr.Input, the SSR
// bundle and the render clock (a request with a clock skips the cache, see renderPage), and
// TestRenderKeyCoversEveryFieldOfTheInput fails for a field of the input that is neither in the key
// nor listed there with the reason it cannot change the HTML of a cached page.
//
// What the framework writes into a page that does not come from the data of the Loads:
//   - the build ID, in the data the page hands to the browser: BuildID
//   - the tags of the scripts and styles of the build, in the SSR bundle: bundle, which holds the
//     asset names, and the same bundle always writes the same ones
//   - the module preloads: Preload, which follows from the build, the matched path and the results
//   - the data of the Loads, in the hydration script and in the markup: Data and Texts
//   - the session envelope, the data-session attribute of <html> and the script and style that
//     read the browser's copy: Session, SessionKey and SessionHead, so two visitors share a page
//     only when the browser gets the same session
//   - the versions of the page in other languages, which the hreflang links and the language switch
//     are made from: Alternates
//   - the head of the matched routes, from their head functions over the data and the URL:
//     Data, Texts and URL
//   - the nonce of the slots that stand for long texts: replaced by the texts before the page
//     leaves the pool, so none stays in the HTML
//   - the marks of the elements whose hashes go into the Content-Security-Policy, with CSP:
//     taken out before the page leaves the pool, so the HTML is that of a render without them.
//     The header follows from the HTML and policy (see policyKey), which the key covers so that a
//     browser holding the page under another policy gets it again with this one
//
// What the application's own code does is another matter: a component that reads the time or a
// random number makes its page differ from one render to the next. tifa dev renders each page
// twice to say so, and RenderCache = false turns the cache off for such a page.
//
// The URL in the key is the request URI as it came, so two spellings of the same query, or a
// tracking parameter, make two entries. That costs hits and never serves a wrong page, since the
// render reads the same URI. Sorting the query and dropping tracking parameters belongs to the
// whole-page cache, which keys by URL.
func renderKey(in ssr.Input, bundle, policy string) string {
	sum := sha256.New()
	for _, part := range []string{in.BuildID, bundle, policy, strconv.FormatBool(in.CSP), in.URL, string(in.Data), in.Preload, string(in.Session), in.SessionKey, in.SessionHead} {
		io.WriteString(sum, part)
		sum.Write([]byte{0})
	}
	// A site without languages hashes as it did before they existed.
	if in.Language != "" {
		io.WriteString(sum, "language:"+in.Language)
		sum.Write([]byte{0})
	}
	if in.Alternates != nil {
		io.WriteString(sum, "alternates:"+string(in.Alternates))
		sum.Write([]byte{0})
	}
	// Each text is written with its length, so moving a character from one text to the next
	// makes another key.
	for _, text := range in.Texts {
		fmt.Fprintf(sum, "%d:", len(text))
		io.WriteString(sum, text)
	}
	return `"` + hex.EncodeToString(sum.Sum(nil)[:16]) + `"`
}

// renderPage renders the document in the VM. A successful page gets an ETag from the hash of
// the build ID, the SSR bundle, the URL and the data, which are all the render depends on. A matching
// If-None-Match is answered 304, and with cache the HTML of an earlier render is reused, or of the
// render that other requests for the same page wait for at the time (renderCached). With
// server.compress the ETag is weak: the page goes out in several encodings under it, and the 304,
// which is answered before the encoding is known, carries the same ETag as the 200. key is the key
// of the page's preloads (see front.preload).
func (s *server) renderPage(c *tifa.Ctx, items map[string]routeResult, since time.Time, status int, cache bool, key string) error {
	r := c.Request()
	data, texts, pageTexts, err := renderInput(items)
	if err != nil {
		return err
	}
	sess, err := s.pageSession(c)
	if err != nil {
		return err
	}
	if sess.json != nil {
		if err := checkSessionCache(c.Writer().Header()); err != nil {
			return err
		}
	}
	// The render runs on the pool of f, which swapFront closes only after this returns. Only
	// tifa dev swaps the front end, so in production the lock is never contended.
	s.rendering.RLock()
	defer s.rendering.RUnlock()
	f := s.front.Load()
	versions, err := s.pageAlternatesFor(c, status)
	if err != nil {
		return err
	}
	alternates, err := alternatesJSON(versions)
	if err != nil {
		return err
	}
	in := ssr.Input{BuildID: f.build.ID, Language: c.Language(), Alternates: alternates, URL: s.renderURI(c), Data: data, Texts: texts, PageTexts: pageTexts, Preload: f.preloadOf(key, items),
		Session: sess.json, SessionKey: sess.key, SessionHead: sess.head, CSP: s.policyHeader() != ""}
	failed := &renderError{f: f, in: in, key: key, loads: since}
	// A render clock makes the HTML depend on a time that is no part of the key, so such a request
	// neither reads the cache nor writes to it, and shares no render.
	if core.RenderClock(r.Context()) != nil {
		cache = false
	}
	// The ETag and the 304 before the render stand on the HTML being a function of the key. A page
	// with RenderCache = false says it is not, and a request with a clock is not either, so they get
	// no ETag and always render: a hash of the input would answer 304 for HTML that has changed.
	var tag string
	if status == http.StatusOK && cache {
		tag = renderKey(in, f.build.bundle, s.policyKey())
		etag := tag
		if s.cfg.Compress {
			etag = "W/" + tag
		}
		c.SetHeader("ETag", etag)
		// An answer that sets the session cookie keeps its status: the cookie must reach the
		// browser, and a 304 is answered to a request that has no use for it.
		if !core.NoStore(c) && etagMatches(c.Header("If-None-Match"), tag) {
			c.SetHeader(renderHeader, "none")
			setDataAge(c.Writer().Header(), time.Since(since))
			// The 304 carries no policy, see dropPagePolicy.
			s.dropPagePolicy(c.Writer().Header())
			return c.NoContent(http.StatusNotModified)
		}
	}
	failed.start = time.Now()
	if cache && tag != "" {
		return s.renderCached(c, f, in, tag, status, failed, since)
	}
	page, err := f.render(r.Context(), in)
	if err != nil {
		return renderFailed(c, err, failed)
	}
	c.SetHeader(renderHeader, "vm")
	setDataAge(c.Writer().Header(), time.Since(since))
	s.setPagePolicy(c.Writer().Header(), s.renderedPolicy(f, in, page))
	return writeHTML(c, status, page.HTML)
}

// dataAgeMetric is the Server-Timing metric that says how old the data of a page may be when the
// answer goes out, in milliseconds: from the start of the first Load that made it. The browser dates
// the matches of the page by it (datePage in js/runtime/browser.tsx), and the time Go writes into the
// page would not do: it is fixed, so that the page depends only on its input, and the clock of the
// server is not the clock of the browser. Navigation data does not carry it: the router dates what
// the browser fetches itself. An answer that a cache may keep and serve again without asking tifa
// does not carry it either (see revalidated): the cache would hand out an age that has grown since.
const dataAgeMetric = "tifa-age"

// setDataAge adds the age of the data of a page to the Server-Timing of its answer, rounded up to a
// millisecond so that the data never looks newer than it is, when the answer is revalidated. Other
// values of the header stay. It runs after the Loads, OnRequest and the layouts set their headers.
func setDataAge(h http.Header, age time.Duration) {
	if !revalidated(h) {
		return
	}
	ms := (max(age, 0) + time.Millisecond - 1) / time.Millisecond
	h.Add("Server-Timing", dataAgeMetric+";dur="+strconv.FormatInt(int64(ms), 10))
}

// revalidated reports whether no cache may reuse an answer with headers h without asking tifa
// first: its Cache-Control (RFC 9111 section 5.2) says no-store, or no-cache without a list of
// fields, or the answer has none and writer.commit sends defaultCacheControl. The writer only ever
// adds the default or makes the header stricter, so h already decides it. A malformed header does
// not count.
func revalidated(h http.Header) bool {
	values := h.Values("Cache-Control")
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		directives, ok := splitDirectives(value)
		if !ok {
			return false
		}
		for _, d := range directives {
			name, _, hasArg := strings.Cut(d, "=")
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "no-store":
				return true
			case "no-cache":
				if !hasArg {
					return true
				}
			}
		}
	}
	return false
}

// errClientGone is what Next returns for a request whose client went away while it waited for
// the render of a page. Server.fail cuts the connection of such a request, without an
// answer, without OnError and without a log line.
var errClientGone = fmt.Errorf("tifa: the client went away: %w", context.Canceled)

// renderCached answers a page from the HTML cache, or from the render
// that every request missing the same key at the time waits for (htmlCache.fetch). Each request
// writes the page with its own headers and in its own encoding, as a cache hit does. With
// server.compress the render compresses the page into both its forms once, so no request
// compresses it again, whatever encoding the request that started the render takes.
func (s *server) renderCached(c *tifa.Ctx, f *front, in ssr.Input, key string, status int, failed *renderError, since time.Time) error {
	method, path := c.Request().Method, c.Request().URL.Path
	// A render that fails once no request waits has no OnError to go to, so it is logged.
	unheard := func(err error) {
		if !errors.As(err, new(*ssr.RedirectError)) {
			core.Logger().ErrorContext(c.Context(), "tifa: a render failed and no request waits for it", "method", method, "path", path, "err", &renderError{err: err, f: f, in: in})
		}
	}
	got, err := s.cache.fetch(c.Request().Context(), key, unheard, func(ctx context.Context) (cachedPage, bool, error) {
		return s.renderOne(ctx, f, in, true, method, path)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) && errors.Is(c.Request().Context().Err(), context.Canceled) {
			return errClientGone
		}
		return renderFailed(c, err, failed)
	}
	c.SetHeader(renderHeader, got.how)
	setDataAge(c.Writer().Header(), time.Since(since))
	s.setPagePolicy(c.Writer().Header(), got.page.policy)
	if !got.held {
		return writeHTML(c, status, got.page.html)
	}
	return writeCached(c, status, got.page, s.formEncoding(c))
}

// renderOne renders in and returns the page as the cache keeps it, and whether the cache can keep
// it. twice has tifa dev render a page the render cache keeps once more to find out whether it
// reads the time or a random number, see devRenderTwice.
func (s *server) renderOne(ctx context.Context, f *front, in ssr.Input, twice bool, method, path string) (cachedPage, bool, error) {
	rendered, err := f.render(ctx, in)
	if err != nil {
		return cachedPage{html: rendered.HTML}, false, err
	}
	if twice && s.dev != nil {
		if warned, differs := s.devRenderTwice(ctx, f, in, rendered, method, path); differs {
			return cachedPage{html: warned.HTML, policy: s.renderedPolicy(f, in, warned)}, false, nil
		}
	}
	html, policy := rendered.HTML, s.renderedPolicy(f, in, rendered)
	page, held := s.newPage(html, policy)
	return page, held, nil
}

// newPage makes the page of html for the cache, and says whether the cache can keep it. With
// server.compress a page of at least gzhttp's minimum size is compressed here, once, whatever the
// request that caused the render takes, since the cache keeps the gzip form alone. The page keeps
// the HTML too for the requests that wait for the render and for the tag of the whole-page cache:
// the cache drops it when it stores the page. A page the cache cannot hold is only the HTML, which
// goes out as it comes.
func (s *server) newPage(html, policy string) (cachedPage, bool) {
	page := cachedPage{html: html, n: len(html), policy: policy}
	if s.cache.size == 0 {
		return page, false
	}
	if s.cfg.Compress {
		page.gzip = compressPage(html)
	}
	if !s.cache.holds(page) {
		page.gzip = nil
		return page, false
	}
	page.body = new(bodyID)
	return page, true
}

// devClockShift is how far tifa dev moves the clock of the second render of a page (see
// devRenderTwice): more than a day, and not a round number of minutes or hours, so that a page
// that shows the date, the time to the minute or "3 hours ago" renders differently.
const devClockShift = 36*time.Hour + 7*time.Minute + 13*time.Second

// devRenderTwice is what tifa dev adds to the render of a page that the render cache keeps: it
// renders the same input again, with the clock of the VM moved on by devClockShift, and when the
// HTML differs, the page read the time or a random number while rendering, which the cache would
// freeze. It then returns the page with the warning in the overlay of the dev client and true, and
// the terminal gets the warning with the place where the two renders part. The page is not cached,
// so each request warns until the page is fixed. A second render that fails is left alone: the
// first one is what the request gets.
func (s *server) devRenderTwice(ctx context.Context, f *front, in ssr.Input, rendered ssr.Page, method, path string) (ssr.Page, bool) {
	later, reads := ssr.WithReads(core.WithRenderClock(ctx, func() time.Time { return time.Now().Add(devClockShift) }))
	again, err := f.render(later, in)
	first, second := rendered.HTML, again.HTML
	if err != nil || second == first {
		return ssr.Page{}, false
	}
	i := 0
	for i < len(first) && i < len(second) && first[i] == second[i] {
		i++
	}
	text := "This page read the time or a random number while rendering, so two renders of the same data differ. " +
		"tifa dev renders the page a second time with the clock 36 hours on, and the two differ. " +
		"Change the component so it renders the same, or write RenderCache = false in route.go. " +
		"The built app would keep the first render for later requests.\n\n" +
		"First render:  " + around(first, i) + "\nSecond render: " + around(second, i)
	if places := reads.Places(); len(places) > 0 {
		text += "\n\nThe code of the app read them here:\n  " + strings.Join(places, "\n  ")
	}
	core.Logger().Warn("tifa: "+text, "method", method, "path", path)
	if s.devErrors != nil {
		s.devErrors.renders.Add(deverrors.Render{Method: method, URL: in.URL, Text: text})
	}
	shown, _ := htmlJSON(map[string]string{"title": "The page renders differently each time", "text": text})
	return beforeBody(rendered, fmt.Sprintf(`<script>window.__tifaDevPageError = %s</script>`, shown)), true
}

// beforeBody returns page with html, scripts Go wrote, at the end of its body, and the hashes of
// those scripts with the hashes of the page.
func beforeBody(page ssr.Page, html string) ssr.Page {
	scripts, _ := ssr.InlineHashes(html)
	page.Scripts = append(slices.Clip(page.Scripts), scripts...)
	if i := strings.LastIndex(page.HTML, "</body>"); i >= 0 {
		page.HTML = page.HTML[:i] + html + page.HTML[i:]
	} else {
		page.HTML += html
	}
	return page
}

// around returns the part of html near byte i, which starts at a rune and ends at one.
func around(html string, i int) string {
	const margin = 60
	start, end := max(i-margin, 0), min(i+margin, len(html))
	for start < len(html) && start > 0 && !utf8.RuneStart(html[start]) {
		start++
	}
	for end < len(html) && !utf8.RuneStart(html[end]) {
		end++
	}
	return strings.ReplaceAll(html[start:end], "\n", " ")
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
func (s *server) formEncoding(c *tifa.Ctx) string {
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
func (s *server) dataOnly(c *tifa.Ctx, items map[string]routeResult, status int, key string) error {
	f := s.front.Load()
	sess, err := s.pageSession(c)
	if err != nil {
		return err
	}
	if sess.json != nil {
		if err := checkSessionCache(c.Writer().Header()); err != nil {
			return err
		}
	}
	versions, err := s.pageAlternatesFor(c, status)
	if err != nil {
		return err
	}
	alternates, err := carriedJSON(versions)
	if err != nil {
		return err
	}
	doc, err := dataOnlyDocument(f, items, key, s.renderURI(c), c.Language(), alternates, sess)
	if err != nil {
		return err
	}
	c.SetHeader(renderHeader, "none")
	s.setPagePolicy(c.Writer().Header(), s.documentPolicy(doc))
	return writeHTML(c, status, doc)
}

// dataOnlyDocument is the document of a page that skips SSR for the page at uri: it loads the
// browser bundle and carries the results of the Loads and the session of the request.
func dataOnlyDocument(f *front, items map[string]routeResult, key, uri, language string, alternates jsontext.Value, sess pageSession) (string, error) {
	injected, err := injectedResults(items)
	if err != nil {
		return "", err
	}
	bootData := map[string]any{"url": uri, "buildId": f.build.ID, "data": injected}
	if language != "" {
		bootData["language"] = language
	}
	if alternates != nil {
		bootData["alternates"] = alternates
	}
	if sess.json != nil {
		bootData["session"], bootData["sessionKey"] = sess.json, sess.key
	}
	boot, err := htmlJSON(bootData)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>` + sess.htmlTag() + `<head><meta charset="utf-8">`)
	b.WriteString(sess.head)
	b.WriteString(f.build.Head)
	b.WriteString(f.preloadOf(key, items))
	b.WriteString(`</head><body><div id="tifa-root"></div>`)
	// htmlJSON escapes <, >, &, U+2028 and U+2029, so the data cannot close the script element.
	fmt.Fprintf(&b, `<script type="application/json" id="tifa-data">%s</script></body></html>`, boot)
	return b.String(), nil
}

// writeCached writes a page of the HTML cache for the encoding formEncoding chose. A page kept as
// gzip goes out as stored, with its length, to a client that takes gzip. A client that takes no
// compression gets the HTML with the Content-Length of the page, unzipped as it is written, and one
// that takes zstd and not gzip, or ranks it higher, gets it as zstd, without a length. Neither is kept. A
// page that has just been rendered has its HTML in hand and sends that, which compressed
// compresses for zstd. A page kept as HTML goes out as HTML, and compressed compresses it as it goes.
func writeCached(c *tifa.Ctx, status int, page cachedPage, encoding string) error {
	if page.gzip == nil {
		return writeHTML(c, status, page.html)
	}
	w := c.Writer()
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	switch {
	case encoding == "gzip":
		h.Set("Content-Encoding", "gzip")
		h.Set("Content-Length", strconv.Itoa(len(page.gzip)))
		w.WriteHeader(status)
		_, err := w.Write(page.gzip)
		return err
	case page.html != "":
		return writeHTML(c, status, page.html)
	}
	// The page is our own bytes. They are read before the header is written, and once it is out a
	// failure cannot be told to the client other than by cutting the connection.
	r, err := gunzipper(page.gzip)
	if err != nil {
		return fmt.Errorf("tifa: a cached page does not unzip: %w", err)
	}
	if encoding == "zstd" {
		h.Set("Content-Encoding", "zstd")
		h.Del("Content-Length") // the zstd form is made as it goes out, its length unknown
	} else {
		h.Set("Content-Length", strconv.Itoa(page.n))
		if c.Request().Method == http.MethodHead {
			r.release()
			w.WriteHeader(status)
			return nil
		}
	}
	w.WriteHeader(status)
	if encoding == "zstd" {
		err = zstdTo(w, r)
	} else {
		err = unzipTo(w, r)
	}
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return nil
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
// key is the key of the page's preloads, and loads when the Loads of items started. When OnRequest
// answers a URL that matches no page with tifa.ErrNotFound itself, no Load ran, and every Load of
// those Layouts counts as a 404. Anything else, a failed OnRequest for one, gets a plain page from
// Go (see fallback). A page whose render
// fails, after OnError has seen that failure, renders again with that error (see rescue). err is
// the error that e stands for.
func (s *server) errorPage(c *tifa.Ctx, e tifa.Error, err error, items map[string]routeResult, loads time.Time, ssrOn bool, key string) {
	if items == nil {
		if c.Pattern() != "" || e.Status != http.StatusNotFound {
			s.fallback(c, e, err)
			return
		}
		// The results stand for the Loads of the Layouts and are made now, which the age of the data
		// of the page counts from.
		loads = time.Now()
		key = s.notFoundAt(c.Request())
		at := s.layouts[key]
		items, ssrOn = make(map[string]routeResult, len(at.chain)), at.ssr
		for _, l := range at.chain {
			items[l.id] = routeResult{Status: e.Status, Error: e.Message}
		}
	}
	var renderErr error
	if ssrOn {
		renderErr = s.renderPage(c, items, loads, e.Status, false, key)
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
func (s *server) rescue(c *tifa.Ctx, e tifa.Error, err error, failed *renderError) {
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
		in.Alternates = nil // an error page has one version
		page, tryErr := failed.f.render(c.Request().Context(), in)
		if tryErr == nil {
			if s.dev != nil {
				page = s.devPageError(c, e, err, page)
			}
			c.SetHeader("Cache-Control", "no-store")
			c.SetHeader(renderHeader, "vm")
			setDataAge(c.Writer().Header(), time.Since(failed.loads))
			s.setPagePolicy(c.Writer().Header(), s.renderedPolicy(failed.f, in, page))
			_ = writeHTML(c, e.Status, page.HTML)
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
	core.Logger().ErrorContext(c.Context(), "tifa: the page with the error failed too", "method", c.Request().Method, "path", c.Request().URL.Path, "err", tries)
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
func (s *server) devPageError(c *tifa.Ctx, e tifa.Error, err error, page ssr.Page) ssr.Page {
	r := c.Request()
	text := fmt.Sprintf("%s %s failed while rendering on the server.\n", r.Method, r.URL.RequestURI())
	if e.Message != http.StatusText(e.Status) {
		text += "Users see: " + e.Message + "\n"
	}
	text += "The page shows the Error component for it. It reloads after you save a fix. The built app shows only the status and the public message.\n\n" + err.Error()
	// htmlJSON escapes <, >, &, U+2028 and U+2029, so the text cannot close the script element.
	shown, _ := htmlJSON(map[string]string{"title": fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status)), "text": text})
	return beforeBody(page, fmt.Sprintf(`<script>window.__tifaDevErrorPage = true; window.__tifaDevPageError = %s</script><script src="%s"></script>`, shown, clientPath))
}

// fallback answers with the plain page of e. Under tifa dev the page also names the request
// and shows err, the error behind e, and it loads the dev client, which reloads the page once
// a rebuild serves a fix. A built app never shows err.
func (s *server) fallback(c *tifa.Ctx, e tifa.Error, err error) {
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
	s.setPagePolicy(w.Header(), s.documentPolicy(b.String()))
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

// renderURI is the URL a page is rendered for. A page with the whole-page cache gets the cleaned
// one (pageURI), the URL its cache entry stands for. The others get the URL as it came.
func (s *server) renderURI(c *tifa.Ctx) string {
	r := c.Request()
	if anonymityOf(r) != nil {
		return s.pageURI(r, c.Pattern())
	}
	return r.URL.RequestURI()
}
