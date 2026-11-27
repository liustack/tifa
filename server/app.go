package server

import (
	"fmt"
	"io/fs"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/core"
)

// App is the application as the generated main describes it: the hooks of hooks.go, the
// Layouts and routes found by the tifa CLI, and the settings of the project tifa.yaml.
type App struct {
	Hooks   Hooks
	Layouts []Layout
	Routes  []Route
	// Settings holds the runtime settings the project tifa.yaml sets, by key, as text, such as
	// "server.addr": "0.0.0.0:9527". tifa build checked them. They are the source below the file
	// of -c and above the defaults.
	Settings map[string]string
	// Session is set when the app calls tifa.SetSession: it tells the process the session type
	// (SessionOf), and the pages, navigation answers and calls carry the session to the browser.
	Session func()
	// I18n holds the languages of the app, nil for an app without a _locales folder.
	I18n *I18n
	// SiteURL is site.url, the scheme and host the site is served at, empty when not set. With it
	// every page gets a canonical link and the server answers sitemap.xml, robots.txt and llms.txt.
	SiteURL string
	// Dist is the .tifa directory's embedded dist/ (the FS holds dist/ at its root): build.json,
	// the SSR bundle, the browser assets and the public files that tifa build writes.
	Dist fs.FS
	// Fingerprint is the hash of the generated table this App comes from, which the server does
	// not read: tifatest compares it with a fresh analysis of the app, to fail a test that would
	// run routes older than the code. Empty for an App written by hand.
	Fingerprint string
}

// I18n is what tifa build found about the languages of the app, see docs/start/app.md.
type I18n struct {
	// Default is the default language, which has no prefix unless Always is set.
	Default string
	// Always is i18n.prefix: always, the URLs of every route that has languages start with the
	// language, the default one too.
	Always bool
	// Languages are the languages of the site, the default one first and then the others in byte
	// order.
	Languages []string
}

// Layout is one directory whose route.go declares Layout, or that holds layout.tsx, or both.
type Layout struct {
	// ID is the directory relative to the application root followed by "/layout", such as
	// "app/projects/project+/layout".
	ID string
	// Path is the path of the directory in ServeMux syntax, such as "/projects/{project}", "/"
	// for app/. A URL that matches no page shows its 404 inside the Layouts whose paths start it
	// (see notFoundAt).
	Path string
	// Load returns the result of the Layout function through tifa.Use, nil when the Layout has none.
	Load func(c *tifa.Ctx) (any, error)
	// Func is the Layout function itself, which Load runs: the runs of the function get the view
	// of the request of this Layout by it (see loadViews).
	Func any
	// HTML is true when the result of the Layout function may hold a tifa.HTML value: a page the
	// server renders carries its data with a marker in place of each one (see pageText).
	HTML bool
	// Languages are the languages of the area the Layout is in, the default one first, nil for a
	// Layout outside every area.
	Languages []string
	// Query lists the query parameters the Layout function takes, the fields of its second
	// parameter, sorted.
	Query []string
	// Whole, Floats and Bools list those of Query that are a whole number, a float64 or a bool, or a
	// list of them, sorted. An empty value of one is a parameter that was not passed, and a number may
	// be written another way than the shortest one (see renderQuery).
	Whole, Floats, Bools []string
}

// Route is one directory holding page.tsx, or a route.go that declares a method handler, or both.
type Route struct {
	// ID is the directory relative to the application root followed by "/route", such as
	// "app/projects/project+/route".
	ID string
	// Path is the path of the directory in ServeMux syntax, such as "/projects/{project}".
	// "/" is the exact home page.
	Path string
	// Page reports whether page.tsx exists. The framework answers GET for a page.
	Page bool
	// Load returns the result of the route's Load through tifa.Use, nil when the route has none.
	Load func(c *tifa.Ctx) (any, error)
	// Func is the Load function itself, as Layout.Func.
	Func any
	// The handlers of route.go, named after their HTTP methods, nil when absent. A page has no GET
	// and no HEAD. Without HEAD, GET answers HEAD. Without OPTIONS, the server answers it with 204
	// and Allow.
	GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS, CONNECT, TRACE func(c *tifa.Ctx) error
	// SSR is false when the route or a Layout in its chain sets SSR = false.
	SSR bool
	// NoMemo is true when route.go sets Memo = false. The HTML of the page is not cached
	// and its renders are not shared. The zero value keeps the render memo on.
	NoMemo bool
	// Cache is how long a shared cache in front of the program may keep the page (s-maxage), from
	// route.go's const Cache = 10 * time.Minute. Zero for none.
	Cache time.Duration
	// CacheDefault is true for const Cache = true: the time is cache.s_maxage.
	CacheDefault bool
	// Layouts lists the IDs of the enclosing Layouts, outermost first.
	Layouts []string
	// HTML is true when the result of Load may hold a tifa.HTML value, as Layout.HTML.
	HTML bool
	// Languages are the languages of the area the route is in, the default one first. A route
	// outside every area has none: it is never prefixed, in both prefix modes.
	Languages []string
	// Query lists the query parameters the Load takes, the fields of its second parameter, sorted.
	Query []string
	// Whole, Floats and Bools list those of Query that are a whole number, a float64 or a bool, or a
	// list of them, sorted.
	Whole, Floats, Bools []string
}

// register adds the Layouts and routes of an application. Like http.ServeMux it panics on an
// inconsistent registration, which the generated code never produces.
func (s *server) register(layouts []Layout, routes []Route) {
	byID := map[string]Layout{}
	for _, l := range layouts {
		if _, dup := byID[l.ID]; dup {
			panic(fmt.Sprintf("tifa: Layout %s registered twice", l.ID))
		}
		if l.Path == "" {
			panic(fmt.Sprintf("tifa: Layout %s has no Path", l.ID))
		}
		byID[l.ID] = l
		if len(l.Languages) > 0 {
			s.layoutLangs[l.ID] = l.Languages
		}
	}
	paths := map[string]string{}
	for _, r := range routes {
		if other, dup := paths[r.Path]; dup {
			panic(fmt.Sprintf("tifa: routes %s and %s share Path %s", other, r.ID, r.Path))
		}
		paths[r.Path] = r.ID
		if len(r.Languages) > 0 {
			s.langs[r.Path] = r.Languages
		}
		var chain []load
		var query []string
		var kinds queryKinds
		for i, id := range r.Layouts {
			l, ok := byID[id]
			if !ok {
				panic(fmt.Sprintf("tifa: route %s names unknown Layout %s", r.ID, id))
			}
			if l.Load != nil {
				chain = append(chain, load{id: l.ID, run: l.Load, html: l.HTML})
			}
			query = append(query, l.Query...)
			kinds.add(l.Whole, l.Floats, l.Bools)
			if _, ok := s.layouts[id]; !ok {
				s.layouts[id] = layoutChain{chain: slices.Clone(chain)}
				s.modules[id] = slices.Clone(r.Layouts[:i+1])
				s.query[id] = sortedSet(query)
				s.kinds[id] = kinds.sorted()
			}
		}
		if r.Load != nil {
			chain = append(chain, load{id: r.ID, run: r.Load, html: r.HTML})
		}
		query = append(query, r.Query...)
		kinds.add(r.Whole, r.Floats, r.Bools)
		if r.Page {
			s.query[r.Path] = sortedSet(query)
			s.kinds[r.Path] = kinds.sorted()
		}
		s.path(r.Path)
		switch {
		case r.Page && r.CacheDefault:
			s.shared[r.Path] = s.sharedDefault
		case r.Page && r.Cache > 0:
			s.shared[r.Path] = r.Cache
		}
		if r.Page {
			if r.GET != nil || r.HEAD != nil {
				panic(fmt.Sprintf("tifa: page %s defines GET or HEAD", r.ID))
			}
			s.pages[r.Path] = true
			if r.NoMemo {
				s.uncached[r.Path] = true
			}
			s.modules[r.Path] = append(slices.Clone(r.Layouts), r.ID)
			s.handle("GET", r.Path, s.pageHandler(r, chain))
		}
		for _, h := range []struct {
			method string
			fn     func(*tifa.Ctx) error
		}{{"GET", r.GET}, {"HEAD", r.HEAD}, {"POST", r.POST}, {"PUT", r.PUT}, {"PATCH", r.PATCH}, {"DELETE", r.DELETE}, {"OPTIONS", r.OPTIONS}, {"CONNECT", r.CONNECT}, {"TRACE", r.TRACE}} {
			if h.fn != nil {
				s.handle(h.method, r.Path, h.fn)
			}
		}
	}
	s.candidates = notFoundCandidates(layouts, routes)
	s.views = loadViews(layouts, routes)
	// A route for the rest of the path, such as /files/{path...}, also answers /files with an
	// empty parameter, unless a route has that Path. The browser router writes the URL with an
	// empty rest that way, and ServeMux alone would redirect it to /files/, which the browser
	// router redirects back.
	for _, r := range routes {
		i := strings.LastIndex(r.Path, "/{")
		if i <= 0 || !strings.HasSuffix(r.Path, "...}") {
			continue
		}
		if prefix := r.Path[:i]; paths[prefix] == "" {
			s.mux.Handle(prefix, routeHandler{s: s, pattern: r.Path, methods: s.routes[r.Path]})
		}
	}
}

// loadViews makes what each Load and Layout function of the app sees of the request, by the code
// pointer of the function, which is how tifa.Use tells their runs apart: the query parameters it
// declared, spelled as the render key spells them, and for a Layout none of the path parameters
// of the folders below it. The browser keeps the data of a Layout while the visitor moves between
// the pages below it, and loads it again when one of its own parameters changes.
func loadViews(layouts []Layout, routes []Route) map[uintptr]*core.LoadView {
	var params []string
	for _, l := range layouts {
		params = append(params, pathParams(l.Path)...)
	}
	for _, r := range routes {
		params = append(params, pathParams(r.Path)...)
	}
	params = sortedSet(params)
	views := map[uintptr]*core.LoadView{}
	add := func(id string, fn any, query, whole, floats, bools []string, hide []string) {
		if fn == nil {
			return
		}
		f := reflect.ValueOf(fn)
		if f.Kind() != reflect.Func {
			panic(fmt.Sprintf("tifa: Func of %s is a %T, not a function", id, fn))
		}
		kinds := queryKinds{whole, floats, bools}.sorted()
		views[f.Pointer()] = &core.LoadView{
			RawQuery: func(u *url.URL) string { return strings.TrimPrefix(renderQuery(u, query, kinds), "?") },
			Hide:     hide,
		}
	}
	for _, l := range layouts {
		own := pathParams(l.Path)
		hide := slices.DeleteFunc(slices.Clone(params), func(p string) bool { return slices.Contains(own, p) })
		add(l.ID, l.Func, sortedSet(l.Query), l.Whole, l.Floats, l.Bools, hide)
	}
	for _, r := range routes {
		add(r.ID, r.Func, sortedSet(r.Query), r.Whole, r.Floats, r.Bools, nil)
	}
	return views
}

// pathParams lists the names of the parameters of a path in ServeMux syntax.
func pathParams(path string) []string {
	var names []string
	for _, seg := range segmentsOf(path) {
		if seg.param && seg.text != "$" {
			names = append(names, seg.text)
		}
	}
	return names
}

// load is one Load of a page's chain.
type load struct {
	id  string
	run func(c *tifa.Ctx) (any, error)
	// html is true when the result may hold a tifa.HTML value, see Route.HTML.
	html bool
}

// runLoads runs the Loads concurrently and returns their results and errors in chain order.
func runLoads(c *tifa.Ctx, chain []load) ([]any, []error) {
	values := make([]any, len(chain))
	errs := make([]error, len(chain))
	var wg sync.WaitGroup
	for i, l := range chain {
		wg.Go(func() {
			// tifa.Use recovers panics of the Load it runs.
			values[i], errs[i] = l.run(c)
		})
	}
	wg.Wait()
	return values, errs
}

func sortedSet(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)
	return slices.Compact(out)
}
