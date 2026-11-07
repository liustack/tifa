package server

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/liustack/tifa"
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
	// Dist is the .tifa directory's embedded dist/ (the FS holds dist/ at its root): build.json,
	// the SSR bundle, the browser assets and the public files that tifa build writes.
	Dist fs.FS
	// Fingerprint is the hash of the generated table this App comes from, which the server does
	// not read: tifatest compares it with a fresh analysis of the app, to fail a test that would
	// run routes older than the code. Empty for an App written by hand.
	Fingerprint string
}

// I18n is what tifa build found about the languages of the app, see docs/app.md.
type I18n struct {
	// Default is the default language, which has no prefix unless Always is set.
	Default string
	// Always is i18n.prefix: always, the URLs of every route that has languages start with the
	// language, the default one too.
	Always bool
	// Languages are the languages of the site, the default one first and then the others in byte
	// order.
	Languages []string
	// SiteURL is site.url, the scheme and host the site is served at, empty when not set.
	SiteURL string
}

// Layout is one directory whose route.go declares Layout or LayoutSSR, or that holds layout.tsx, or both.
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
	// SSR is the Layout's own LayoutSSR constant.
	SSR bool
	// HTML is true when the result of the Layout function may hold a tifa.HTML value: a page the
	// server renders carries its data with a marker in place of each one (see pageText).
	HTML bool
	// Languages are the languages of the area the Layout is in, the default one first, nil for a
	// Layout outside every area.
	Languages []string
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
	// The handlers of route.go, named after their HTTP methods, nil when absent. A page has no GET.
	GET, POST, PUT, PATCH, DELETE func(c *tifa.Ctx) error
	// SSR is false when the route or a Layout in its chain sets SSR = false.
	SSR bool
	// NoRenderCache is true when route.go sets RenderCache = false. The HTML of the page is not cached
	// and its renders are not shared. The zero value keeps the render cache on.
	NoRenderCache bool
	// Cache is the whole-page cache time of the page, route.go's const Cache, zero for none.
	Cache time.Duration
	// Query lists the query parameters the Loads of the page and of the Layouts around it read,
	// sorted. The whole-page cache keeps them in its key and in the URL it renders for, even
	// those cache.ignore_query names.
	Query []string
	// BodyLimit and ReadTimeout are route.go's BodyLimit and ReadTimeout: the body limit and the read
	// timeout of the requests the method handlers of the route answer, in place of
	// server.body_limit and server.read_timeout. The answer then gets server.write_timeout after
	// ReadTimeout. Zero keeps the setting, so an option of the app that is 0 means the default.
	BodyLimit   int64
	ReadTimeout time.Duration
	// Layouts lists the IDs of the enclosing Layouts, outermost first.
	Layouts []string
	// HTML is true when the result of Load may hold a tifa.HTML value, as Layout.HTML.
	HTML bool
	// Languages are the languages of the area the route is in, the default one first. A route
	// outside every area has none: it is never prefixed, in both prefix modes.
	Languages []string
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
		if r.BodyLimit < 0 || r.ReadTimeout < 0 {
			panic(fmt.Sprintf("tifa: route %s has a negative BodyLimit or ReadTimeout", r.ID))
		}
		if r.BodyLimit > 0 || r.ReadTimeout > 0 {
			s.limits[r.Path] = routeLimits{body: r.BodyLimit, read: r.ReadTimeout}
		}
		var chain []load
		ssrOn := true
		for i, id := range r.Layouts {
			l, ok := byID[id]
			if !ok {
				panic(fmt.Sprintf("tifa: route %s names unknown Layout %s", r.ID, id))
			}
			if l.Load != nil {
				chain = append(chain, load{id: l.ID, run: l.Load, html: l.HTML})
			}
			ssrOn = ssrOn && l.SSR
			if _, ok := s.layouts[id]; !ok {
				s.layouts[id] = layoutChain{chain: slices.Clone(chain), ssr: ssrOn}
				s.modules[id] = slices.Clone(r.Layouts[:i+1])
			}
		}
		if r.Load != nil {
			chain = append(chain, load{id: r.ID, run: r.Load, html: r.HTML})
		}
		s.path(r.Path)
		if r.Page && r.Cache > 0 && s.wholePage {
			s.pageCache[r.Path] = r.Cache
			s.pageQuery[r.Path] = r.Query
		}
		if r.Page {
			if r.GET != nil {
				panic(fmt.Sprintf("tifa: page %s defines GET", r.ID))
			}
			s.pages[r.Path] = true
			s.modules[r.Path] = append(slices.Clone(r.Layouts), r.ID)
			s.handle("GET", r.Path, s.pageHandler(r, chain))
		}
		for _, h := range []struct {
			method string
			fn     func(*tifa.Ctx) error
		}{{"GET", r.GET}, {"POST", r.POST}, {"PUT", r.PUT}, {"PATCH", r.PATCH}, {"DELETE", r.DELETE}} {
			if h.fn != nil {
				s.handle(h.method, r.Path, h.fn)
			}
		}
	}
	s.candidates = notFoundCandidates(layouts, routes)
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
