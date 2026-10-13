package server

import (
	"fmt"
	"io/fs"
	"sync"

	"github.com/liustack/tifa"
)

// App is the application as the generated main describes it: the hooks of hooks.go, the
// Layouts and routes found by the tifa CLI, and the project tifa.yaml embedded at build time.
type App struct {
	Hooks   Hooks
	Layouts []Layout
	Routes  []Route
	// ConfigName is "tifa.yaml" or "tifa.json" when the project has a config file, empty otherwise.
	// Config is the file's content. It is the layer below --config and above the defaults.
	ConfigName string
	Config     []byte
	// Dist is the .tifa directory's embedded dist/ (the FS holds dist/ at its root): build.json,
	// the SSR bundle, the browser assets and the public files that tifa build writes.
	Dist fs.FS
}

// Layout is one directory holding layout.go, layout.tsx or both.
type Layout struct {
	// ID is the directory relative to the application root, such as "app/projects".
	ID string
	// Load returns the result of the Layout's Load through tifa.Use, nil when the Layout has none.
	Load func(c *tifa.Ctx) (any, error)
	// SSR is the Layout's own SSR constant.
	SSR bool
}

// Route is one directory holding route.go.
type Route struct {
	// ID is the directory relative to the application root, such as "app/projects/list".
	ID string
	// Path is the Path constant as written. "/" is the exact home page.
	Path string
	// Page reports whether route.tsx exists. The framework answers GET for a page.
	Page bool
	// Load returns the result of the route's Load through tifa.Use, nil when the route has none.
	Load func(c *tifa.Ctx) (any, error)
	// The OnXxx functions of route.go, nil when absent. A page has no OnGet.
	OnGet, OnPost, OnPut, OnPatch, OnDelete func(c *tifa.Ctx) error
	// SSR is false when the route or a Layout in its chain sets SSR = false.
	SSR bool
	// Cache is the route's Cache constant.
	Cache bool
	// Layouts lists the IDs of the enclosing Layouts, outermost first.
	Layouts []string
}

// register adds the Layouts and routes of an application. Like http.ServeMux it panics on an
// inconsistent registration, which the generated code never produces.
func (s *Server) register(layouts []Layout, routes []Route) {
	byID := map[string]Layout{}
	for _, l := range layouts {
		if _, dup := byID[l.ID]; dup {
			panic(fmt.Sprintf("tifa: Layout %s registered twice", l.ID))
		}
		byID[l.ID] = l
	}
	paths := map[string]string{}
	for _, r := range routes {
		if other, dup := paths[r.Path]; dup {
			panic(fmt.Sprintf("tifa: routes %s and %s share Path %s", other, r.ID, r.Path))
		}
		paths[r.Path] = r.ID
		var chain []load
		for _, id := range r.Layouts {
			l, ok := byID[id]
			if !ok {
				panic(fmt.Sprintf("tifa: route %s names unknown Layout %s", r.ID, id))
			}
			if l.Load != nil {
				chain = append(chain, load{id: l.ID, run: l.Load})
			}
		}
		if r.Load != nil {
			chain = append(chain, load{id: r.ID, run: r.Load})
		}
		s.path(r.Path)
		if r.Page {
			if r.OnGet != nil {
				panic(fmt.Sprintf("tifa: page %s defines OnGet", r.ID))
			}
			s.Handle("GET", r.Path, pageHandler(chain))
		}
		for _, h := range []struct {
			method string
			fn     func(*tifa.Ctx) error
		}{{"GET", r.OnGet}, {"POST", r.OnPost}, {"PUT", r.OnPut}, {"PATCH", r.OnPatch}, {"DELETE", r.OnDelete}} {
			if h.fn != nil {
				s.Handle(h.method, r.Path, h.fn)
			}
		}
	}
}

// load is one Load of a page's chain.
type load struct {
	id  string
	run func(c *tifa.Ctx) (any, error)
}

// pageHandler answers a page GET without a Renderer: the results of every Load in the chain as
// JSON by ID. Whole-page rendering replaces it once the server has a Renderer.
func pageHandler(chain []load) func(c *tifa.Ctx) error {
	return func(c *tifa.Ctx) error {
		data, err := runLoads(c, chain)
		if err != nil {
			return err
		}
		return c.JSON(200, data)
	}
}

// runLoads runs the Loads concurrently. The results are keyed by ID. With several errors the
// one of the outermost Load wins, so the answer does not depend on timing.
func runLoads(c *tifa.Ctx, chain []load) (map[string]any, error) {
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
	data := make(map[string]any, len(chain))
	for i, l := range chain {
		if errs[i] != nil {
			return nil, errs[i]
		}
		data[l.id] = values[i]
	}
	return data, nil
}
