package server

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/liustack/tifa"
)

// A URL that matches no page shows a 404 inside the Layouts of the deepest folder it matches,
// as the browser router shows it after a navigation (notFoundMode 'fuzzy' and the routes tifa
// build adds, see tanstackRoutes in internal/codegen). Both must pick the same Layout, or the
// router would ask for data of Layouts Go did not load.

// layoutChain is what a URL that shows its 404 inside a Layout loads: the Loads of the Layouts
// from the root down to it, and whether all of them render on the server.
type layoutChain struct {
	chain []load
	ssr   bool
}

// candidate is a route the browser router matches a URL with in part: a Layout, or a page in
// a folder below its Layout. Neither takes the rest of the path, which matches every URL below
// it in full. layout is the ID of the Layout the 404 shows inside, empty for none, and params the
// names of the parameters of that Layout's path by their segment.
type candidate struct {
	segs   []segment
	layout string
	params map[int]string
}

// segment is one segment of a ServeMux path: a literal, or a parameter.
type segment struct {
	text  string
	param bool
}

func segmentsOf(path string) []segment {
	var segs []segment
	for _, s := range strings.Split(strings.Trim(path, "/"), "/") {
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "{") {
			segs = append(segs, segment{text: strings.TrimSuffix(strings.Trim(s, "{}"), "..."), param: true})
		} else {
			segs = append(segs, segment{text: s})
		}
	}
	return segs
}

func takesRest(path string) bool { return strings.HasSuffix(path, "...}") }

// notFoundCandidates lists the candidates of the Layouts and pages of an application.
func notFoundCandidates(layouts []Layout, routes []Route) []candidate {
	paths := map[string]string{}
	for _, l := range layouts {
		paths[l.ID] = l.Path
	}
	of := func(segs []segment, layout string) candidate {
		c := candidate{segs: segs, layout: layout, params: map[int]string{}}
		for i, s := range segmentsOf(paths[layout]) {
			if s.param {
				c.params[i] = s.text
			}
		}
		return c
	}
	var out []candidate
	for _, l := range layouts {
		if !takesRest(l.Path) {
			out = append(out, of(segmentsOf(l.Path), l.ID))
		}
	}
	for _, r := range routes {
		var layout string
		if len(r.Layouts) > 0 {
			layout = r.Layouts[len(r.Layouts)-1]
		}
		if r.Page && !takesRest(r.Path) && (layout == "" || paths[layout] != r.Path) {
			out = append(out, of(segmentsOf(r.Path), layout))
		}
	}
	return out
}

// notFoundAt returns the ID of the Layout the 404 of r shows inside, empty for none, and sets the
// values of that Layout's parameters on r, for its Loads to read. It is the Layout of the candidate
// whose path starts the path of r and matches it best: comparing segment by segment, a literal
// beats a parameter, and a parameter beats the end of the path.
func (s *server) notFoundAt(r *http.Request) string {
	var parts []string
	escaped := r.URL.EscapedPath()
	if info := langInfoOf(r); info != nil && info.prefixed {
		escaped = info.rest // the layouts have paths without the language
	}
	for _, p := range strings.Split(strings.TrimPrefix(escaped, "/"), "/") {
		if v, err := url.PathUnescape(p); err == nil {
			p = v
		}
		parts = append(parts, p)
	}
	starts := func(c candidate) bool {
		if len(c.segs) > len(parts) {
			return false
		}
		for i, seg := range c.segs {
			if seg.param && parts[i] == "" || !seg.param && seg.text != parts[i] {
				return false
			}
		}
		return true
	}
	rank := func(c candidate, i int) int {
		switch {
		case i >= len(c.segs):
			return 0
		case c.segs[i].param:
			return 1
		}
		return 2
	}
	better := func(a, b candidate) bool {
		for i := range max(len(a.segs), len(b.segs)) {
			if ra, rb := rank(a, i), rank(b, i); ra != rb {
				return ra > rb
			}
		}
		return false
	}
	var best *candidate
	for i, c := range s.candidates {
		if starts(c) && (best == nil || better(c, *best)) {
			best = &s.candidates[i]
		}
	}
	if best == nil {
		// Only an application without a root Layout has no candidate that starts every path.
		return ""
	}
	for i, name := range best.params {
		r.SetPathValue(name, parts[i])
	}
	return best.layout
}

// notFoundPage is what Next returns for a URL that matches no page once the Loads of the Layouts
// it shows inside have run: tifa.ErrNotFound, with their results, which Server.fail answers with a
// 404 inside those Layouts, as JSON for a data request.
type notFoundPage struct {
	items  map[string]routeResult
	loads  time.Time // when the Loads started, see runChain
	ssr    bool
	layout string
	data   bool
}

func (p *notFoundPage) Error() string { return tifa.ErrNotFound.Error() }
func (p *notFoundPage) Unwrap() error { return tifa.ErrNotFound }

// unmatched answers a URL that matches no route. A GET or HEAD of an application with pages runs
// the Loads of the Layouts its 404 shows inside (notFoundAt), all of them for a page and those that
// _tifa asks for in a data request, as a page runs those of its chain. Anything else is
// tifa.ErrNotFound. c.Pattern() stays empty.
func (s *server) unmatched(c *tifa.Ctx) error {
	r := c.Request()
	if !s.pagesServed() || r.Method != "GET" && r.Method != "HEAD" {
		return tifa.ErrNotFound
	}
	layout := s.notFoundAt(r)
	if info := langInfoOf(r); info != nil && !s.hasLanguage(layout, info) {
		return tifa.ErrNotFound
	}
	at := s.layouts[layout]
	if c.IsData() {
		selected, err := s.listedLoads(c, at.chain)
		if err != nil {
			return err
		}
		items, _, err := runChain(c, selected, false, layout)
		if err != nil {
			return err
		}
		return &notFoundPage{items: items, data: true}
	}
	items, loads, err := runChain(c, at.chain, at.ssr, layout)
	if err != nil {
		return err
	}
	return &notFoundPage{items: items, loads: loads, ssr: at.ssr, layout: layout}
}
