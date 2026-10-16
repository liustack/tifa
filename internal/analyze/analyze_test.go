package analyze

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func analyzeApp(t *testing.T, name string) (*Manifest, error) {
	t.Helper()
	return Analyze(filepath.Join("testdata", name))
}

func TestValidApp(t *testing.T) {
	m, err := analyzeApp(t, "valid")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if m.Module != "example.com/valid" {
		t.Errorf("Module = %q", m.Module)
	}
	if want := []string{"OnStart", "OnRequest"}; !reflect.DeepEqual(m.Hooks, want) {
		t.Errorf("Hooks = %v, want %v", m.Hooks, want)
	}

	layouts := map[string]Layout{}
	for _, l := range m.Layouts {
		layouts[l.ID] = l
	}
	if len(layouts) != 4 {
		t.Fatalf("got %d layouts: %+v", len(m.Layouts), m.Layouts)
	}
	const project = "app/projects/projectId+"
	wantLayouts := []struct {
		id, dir, parent, path, pkg string
		ssr, tsx, load             bool
		routes                     []string
	}{
		{"app/layout", "app", "", "/", "example.com/valid/app", true, true, true,
			[]string{"app/route", "app/api/health/route", "app/auth/login/route", "app/auth/register/route", "app/files/path++/route",
				project + "/route", project + "/export/route", project + "/orders/orderId+/route"}},
		{"app/auth/layout", "app/auth", "app/layout", "/auth", "example.com/valid/app/auth", true, true, true,
			[]string{"app/auth/login/route", "app/auth/register/route"}},
		{"app/files/layout", "app/files", "app/layout", "/files", "", true, true, false, []string{"app/files/path++/route"}},
		{project + "/layout", project, "app/layout", "/projects/{projectId}", "example.com/valid/" + project, false, true, true,
			[]string{project + "/route", project + "/export/route", project + "/orders/orderId+/route"}},
	}
	for _, w := range wantLayouts {
		l := layouts[w.id]
		if l.Dir != w.dir || l.Parent != w.parent || l.Path != w.path || l.Package != w.pkg || l.SSR != w.ssr || l.TSX != w.tsx || (l.Load != nil) != w.load {
			t.Errorf("layout %s = %+v, want %+v", w.id, l, w)
		}
		if !reflect.DeepEqual(l.Routes, w.routes) {
			t.Errorf("layout %s routes = %v, want %v", w.id, l.Routes, w.routes)
		}
	}
	if got := layouts[project+"/layout"].Load; got == nil || got.Type != "example.com/valid/"+project+".Project" ||
		!reflect.DeepEqual(got.Params, []string{"projectId"}) || !reflect.DeepEqual(got.Query, []string{"currency"}) ||
		!reflect.DeepEqual(got.AllQuery, []string{"currency", "lang"}) || !reflect.DeepEqual(got.Uses, []string{"app/layout"}) {
		t.Errorf("project layout Load = %+v", got)
	}
	if got := layouts["app/layout"].Load; got == nil || !reflect.DeepEqual(got.Query, []string{"lang"}) ||
		!reflect.DeepEqual(got.AllQuery, []string{"lang"}) || len(got.Uses) != 0 {
		t.Errorf("root layout Load = %+v", got)
	}

	routes := map[string]Route{}
	for _, r := range m.Routes {
		routes[r.ID] = r
	}
	if len(routes) != 8 {
		t.Fatalf("got %d routes: %+v", len(m.Routes), m.Routes)
	}
	wantRoutes := []struct {
		id, dir, path    string
		page, ssr, cache bool
		methods, layouts []string
	}{
		{"app/route", "app", "/", true, true, false, nil, []string{"app/layout"}},
		{"app/auth/login/route", "app/auth/login", "/auth/login", true, true, false, []string{"POST"}, []string{"app/layout", "app/auth/layout"}},
		{"app/auth/register/route", "app/auth/register", "/auth/register", true, true, false, nil, []string{"app/layout", "app/auth/layout"}},
		{project + "/route", project, "/projects/{projectId}", true, false, true, nil, []string{"app/layout", project + "/layout"}},
		{project + "/orders/orderId+/route", project + "/orders/orderId+", "/projects/{projectId}/orders/{orderId}", true, false, false,
			[]string{"POST", "DELETE"}, []string{"app/layout", project + "/layout"}},
		{"app/files/path++/route", "app/files/path++", "/files/{path...}", true, true, false, nil, []string{"app/layout", "app/files/layout"}},
		{"app/api/health/route", "app/api/health", "/api/health", false, true, false, []string{"GET"}, []string{"app/layout"}},
		{project + "/export/route", project + "/export", "/projects/{projectId}/export", false, false, false, []string{"GET"}, []string{"app/layout", project + "/layout"}},
	}
	for _, w := range wantRoutes {
		r := routes[w.id]
		if r.Dir != w.dir || r.Path != w.path || r.Page != w.page || r.SSR != w.ssr || r.Cache != w.cache ||
			!reflect.DeepEqual(handlerMethods(r), w.methods) || !reflect.DeepEqual(r.Layouts, w.layouts) {
			t.Errorf("route %s = %+v, want %+v", w.id, r, w)
		}
	}

	if r := routes["app/route"]; r.Load == nil || r.Load.Type != "string" || !reflect.DeepEqual(r.Load.Query, []string{"ref"}) ||
		!reflect.DeepEqual(r.Load.AllQuery, []string{"ref"}) {
		t.Errorf("home Load = %+v", r.Load)
	}
	// The project page reads page and sort itself, and through tifa.Use(c, Layout) also depends on
	// currency and, one level further up, lang.
	if r := routes[project+"/route"]; r.Load == nil || r.Load.Type != "[]string" ||
		!reflect.DeepEqual(r.Load.Query, []string{"page", "sort"}) || len(r.Load.Params) != 0 ||
		!reflect.DeepEqual(r.Load.AllQuery, []string{"currency", "lang", "page", "sort"}) ||
		!reflect.DeepEqual(r.Load.Uses, []string{project + "/layout"}) {
		t.Errorf("project page Load = %+v", r.Load)
	}
	detail := routes[project+"/orders/orderId+/route"]
	if detail.Load == nil || detail.Load.Type != "*example.com/valid/"+project+"/orders/orderId+.Order" ||
		!reflect.DeepEqual(detail.Load.Params, []string{"orderId", "projectId"}) {
		t.Errorf("detail Load = %+v", detail.Load)
	}
	if detail.Package != "example.com/valid/"+project+"/orders/orderId+" {
		t.Errorf("detail Package = %q", detail.Package)
	}
	if r := routes["app/auth/register/route"]; r.Load != nil {
		t.Errorf("register Load = %+v, want nil", r.Load)
	}
}

// Domain code lives in the folders under app/: a package may hold a domain and a page, or only a
// domain, packages under app/ import each other in one direction, and the domains import the
// root package for the database. The analyzer takes all of it as a plain app.
func TestDomainsUnderApp(t *testing.T) {
	m, err := analyzeApp(t, "domains")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if want := []string{"OnStart"}; !reflect.DeepEqual(m.Hooks, want) {
		t.Errorf("Hooks = %v, want %v", m.Hooks, want)
	}
	const pkg = "example.com/domains/"
	const project = "app/projects/project+"
	wantRoutes := map[string]struct{ pkg, load string }{
		"app/customers/route":            {pkg + "app/customers", "[]" + pkg + "app/customers.Customer"},
		"app/projects/route":             {pkg + "app/projects", "[]" + pkg + "app/projects.Project"},
		project + "/orders/order+/route": {pkg + project + "/orders/order+", pkg + project + "/orders.Order"},
	}
	if len(m.Routes) != len(wantRoutes) {
		t.Fatalf("got %d routes: %+v", len(m.Routes), m.Routes)
	}
	for _, r := range m.Routes {
		w, ok := wantRoutes[r.ID]
		if !ok || r.Package != w.pkg || r.Load == nil || r.Load.Type != w.load {
			t.Errorf("route %s = %+v, Load %+v, want %+v", r.ID, r, r.Load, w)
		}
	}
	detail := project + "/orders/order+/route"
	for _, r := range m.Routes {
		if r.ID == detail && (!reflect.DeepEqual(r.Load.Uses, []string{project + "/layout"}) ||
			!reflect.DeepEqual(r.Load.Params, []string{"order"})) {
			t.Errorf("detail Load = %+v", r.Load)
		}
	}
	layouts := map[string]string{}
	for _, l := range m.Layouts {
		if l.Load != nil {
			layouts[l.ID] = l.Load.Type
		}
	}
	if want := map[string]string{"app/layout": "[]" + pkg + "app/projects.Project", project + "/layout": pkg + "app/projects.Project"}; !reflect.DeepEqual(layouts, want) {
		t.Errorf("Layout types = %v, want %v", layouts, want)
	}
}

func handlerMethods(r Route) []string {
	var methods []string
	for _, h := range r.Handlers {
		methods = append(methods, h.Method)
	}
	return methods
}

// A method handler takes an optional input after the Ctx and returns an optional result before
// the error. The input and the result are recorded with their Go types.
func TestTypedHandlers(t *testing.T) {
	m, err := analyzeApp(t, "handlers")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	const orders = "example.com/handlers/app/orders."
	const note = "example.com/handlers/app/note."
	want := map[string][]Handler{
		"app/orders/route": {
			{Method: "GET", Input: orders + "Filter", Output: "[]" + orders + "Order"},
			{Method: "POST", Input: orders + "ShipInput"},
			{Method: "PUT", Output: orders + "Order"},
			{Method: "PATCH", Input: orders + "ShipInput", Output: "*" + orders + "Order"},
			{Method: "DELETE"},
		},
		"app/note/route": {
			{Method: "POST", Input: note + "note", Output: note + "note"},
			{Method: "PUT", Input: "struct{Text string \"json:\\\"text\\\"\"}"},
		},
	}
	for _, r := range m.Routes {
		got := make([]Handler, len(r.Handlers))
		for i, h := range r.Handlers {
			if (h.Input == "") != (h.InputType == nil) || (h.Output == "") != (h.OutputType == nil) {
				t.Errorf("%s %s: types %v %v do not match %q %q", r.ID, h.Method, h.InputType, h.OutputType, h.Input, h.Output)
			}
			got[i] = Handler{Method: h.Method, Input: h.Input, Output: h.Output}
		}
		if !reflect.DeepEqual(got, want[r.ID]) {
			t.Errorf("%s handlers = %+v, want %+v", r.ID, got, want[r.ID])
		}
	}
}

// Each violation must be reported with its file and line. want lists, per expected error,
// substrings that one single error message must all contain.
func TestViolations(t *testing.T) {
	cases := []struct {
		app  string
		want [][]string
	}{
		{"duppath", [][]string{
			{"app/items/key+/route.go:1:", "app/items/id+ and app/items/key+", "parameter directories"},
			{"app/shops/shop+/a/route.tsx:1:", "app/shops/rest++ and app/shops/shop+", "parameter directories"},
		}},
		{"ongetpage", [][]string{{"app/page/route.go:7:", "GET", "route.tsx"}}},
		{"apiload", [][]string{{"app/api/route.go:7:", "Load", "route.tsx"}}},
		{"badhandler", [][]string{
			{"app/api/route.go:15:", "Validate", "func() error"},
			{"app/api/route.go:19:", "GET input", "Nested.Inner", "query string"},
			{"app/api/route.go:21:", "POST input", "*example.com/badhandler/app/api.Tagged", "struct"},
			{"app/api/route.go:23:", "PUT input", "Tagged.Name", "form tag", "app/api/route.go:10"},
			{"app/api/route.go:27:", "DELETE input", "Embeds.Tagged", "embedded"},
			{"app/api/route.go:27:", "DELETE input", "Embeds.Tagged.Name", "form tag"},
			{"app/more/route.go:15:", "GET input", ".Data", "query string"},
			{"app/more/route.go:17:", "POST input", "Iface.V", "interface"},
			{"app/more/route.go:19:", "PUT result", "chan"},
			{"app/more/route.go:21:", "PATCH input", "Deep.Inner.N", "form tag"},
			{"app/more/route.go:23:", "DELETE must have one of the signatures"},
			{"app/text/route.go:57:", "POST input", "Input.ID", "UnmarshalText"},
			{"app/text/route.go:57:", "POST input", "Input.Codes[]", "UnmarshalText", "not string"},
			{"app/text/route.go:57:", "POST input", "Input.Raw", "UnmarshalJSON"},
			{"app/text/route.go:57:", "POST input", "Input.ByID[key]", "UnmarshalText"},
			{"app/text/route.go:97:", "GET input", "text.Word", "MarshalText", "query string"},
		}},
		{"dirnames", [][]string{
			{"app/a%b/route.tsx:1:", `"%"`},
			{"app/1x+/route.tsx:1:", "parameter name"},
			{"app/rest++/more/route.tsx:1:", "rest of the path"},
			{"app/id+/x/id+/route.tsx:1:", "twice"},
			{"app/x~1/route.tsx:1:", "x~1"},
		}},
		{"layoutparam", [][]string{{"app/projects/layout.go:6:", `"projectId"`, `"/projects"`}}},
		{"usenonancestor", [][]string{
			{"app/shop/route.go:11:", "admin.Layout", "does not wrap"},
			{"app/shop/layout.go:10:", "tifa.Use(c, Layout)", "its own result"},
		}},
		{"useclosure", [][]string{
			{"app/page/route.go:12:", "package-level function", "closure"},
			{"app/page/route.go:13:", "package-level function", "method"},
		}},
		{"badtype", [][]string{
			{"app/page/route.go:56:", "Data.Updates", "chan int"},
			{"app/page/route.go:56:", "Data.ByScore", "map[float64]string"},
			{"app/page/route.go:56:", "Data.Items[].Extra", "any"},
			{"app/page/route.go:56:", "Data.Raw", "json.RawMessage", "MarshalJSON"},
			{"app/page/route.go:56:", "Data.Bytes", "named byte"},
			{"app/page/route.go:56:", "Data.Ptrs[]", "MarshalJSON"},
			{"app/page/route.go:56:", "Data.Text", "interface"},
			{"app/page/route.go:56:", "Data.Bad", "bad'name"},
			{"app/page/route.go:56:", "Data.Append", "AppendText"},
			{"app/page/route.go:56:", "Data.Stream", "MarshalJSONTo"},
		}},
		{"badshape", [][]string{
			{"app/page/route.go:100:", "Data.Both", "MarshalJSONTo"},
			{"app/page/route.go:100:", "Data.Bytes", "named byte"},
			{"app/page/route.go:100:", "Data.Plain", "named byte"},
			{"app/page/route.go:100:", "Data.Embed", `"embed"`},
			{"app/page/route.go:100:", "Data.Hex", `"format:hex"`},
			{"app/page/route.go:100:", "Data.Oops", `"string:oops"`},
			{"app/page/route.go:100:", "Data.Unknown", `"inline"`},
			{"app/page/route.go:100:", "Data.Comma", `""`},
			{"app/page/route.go:100:", "Data.Twice", "twice"},
			{"app/page/route.go:100:", "Data.Named", "string option"},
			{"app/page/route.go:100:", "Data.ByText", "interface"},
			{"app/page/route.go:100:", "Data.Wrong", "MarshalJSONTo", "rename"},
			{"app/page/route.go:100:", "Data.Shadow", "MarshalText", "signature"},
			{"app/page/route.go:100:", "Data.Prom", "MarshalText", "embedded"},
			{"app/page/route.go:100:", "Data.Ptr", "MarshalText", "pointer"},
			{"app/page/route.go:100:", "Data.ByKey", "MarshalText"},
			{"app/page/route.go:100:", "Data.App", "AppendJSON"},
			{"app/page/route.go:100:", "Data.Stamp", "MarshalJSON"},
			{"app/page/route.go:100:", "Data.Num", "string option"},
			{"app/page/route.go:100:", "Data.ByPtr", "pointer"},
			{"app/page/route.go:100:", "Data.secret", "unexported embedded"},
		}},
		{"badpath", [][]string{
			{"app/a/b+/route.go:1:", `"/a/{b}"`, `"/{a}/b"`, "conflicts"},
			{"app/a/b+/c/route.go:1:", `"/a/{b}/c"`, `"/{a}/b/c"`, "conflicts"},
			{"app/d/b/z+/route.go:1:", `"/d/b/{z}"`, `"/{a}/b/c"`, "conflicts"},
			{"app/d/q+/rest++/route.go:1:", `"/d/{q}/{rest...}"`, `"/d/{q}"`, `"/{a}/b"`, "conflicts"},
			{"app/d/q+/rest++/route.go:1:", `"/d/{q}/{rest...}" conflicts`, `"/{a}/b/c"`},
			{"app/declared/route.go:3:", "Path is not declared"},
		}},
		{"paramname", [][]string{
			{"app/items/key+/route.go:13:", `"id"`, `"/items/{key}"`},
			{"app/items/key+/route.go:13:", "c.Query must", "constant"},
			{"app/items/key+/route.go:13:", "c.QueryAll must", "constant"},
		}},
		{"indirect", [][]string{
			{"app/page/route.go:17:", "c.Query", "called directly"},
			{"app/page/route.go:18:", "(*tifa.Ctx).Query", "method expression"},
			{"app/page/route.go:19:", "tifa.Use", "called directly"},
			{"app/page/route.go:20:", "tifa.Use", "called directly"},
			{"app/page/route.go:21:", "use", "function value"},
			{"app/page/route.go:27:", "c.Param", "called directly"},
			{"app/page/route.go:29:", "c.QueryAll", "called directly"},
		}},
		{"ifacectx", [][]string{
			{"app/page/route.go:18:", "converted to interface{Query(string) string}", "call its methods directly"},
			{"app/page/route.go:19:", "variable of type page.querier"},
			{"app/page/route.go:20:", "passed as page.querier to page.read"},
			{"app/page/route.go:21:", "returned"},
			{"app/page/route.go:22:", "element of []page.querier"},
			{"app/page/route.go:23:", "variable of type any"},
			{"app/page/route.go:24:", "converted to interface{Context() context.Context}"},
		}},
		{"escape", [][]string{
			{"app/page/route.go:11:", "c.Request", "c.Query"},
			{"app/page/route.go:12:", "c.Writer", "OnRequest"},
			{"db/db.go:8:", "c.Request"},
			{"db/db.go:10:", "c.Writer"},
		}},
		{"bindquery", [][]string{
			{"app/list/route.go:12:", "c.BindQuery", "c.Query(\"name\")", "c.QueryAll(\"name\")"},
			{"filter/filter.go:12:", "c.BindQuery"},
			{"app/list/route.go:20:", "c.Bind", "request body", "c.Query"},
		}},
		{"ctxflow", [][]string{
			{"app/page/route.go:26:", "element of map[int]*tifa.Ctx"},
			{"app/page/route.go:27:", "converted to any"},
			{"app/page/route.go:28:", "variable of type any"},
			{"app/page/route.go:29:", "element of []*tifa.Ctx"},
			{"app/page/route.go:30:", "sent on a channel"},
			{"app/page/route.go:31:", "[]*tifa.Ctx", "may not hold the Ctx"},
			{"app/page/route.go:33:", "field V of type any"},
			{"app/page/route.go:34:", "element of page.box[*tifa.Ctx]"},
			{"app/page/route.go:35:", "address taken"},
			{"app/page/route.go:37:", "existing variable"},
			{"app/page/route.go:38:", "existing variable"},
			{"app/page/route.go:39:", "package-level variable"},
			{"app/page/route.go:40:", "defer statement"},
			{"app/page/route.go:41:", "defer statement"},
			{"app/page/route.go:42:", "several values"},
			{"app/page/route.go:43:", "returned"},
			{"app/page/route.go:44:", "passed as any to page.takeAny"},
			{"app/page/route.go:44:", "generic function page.generic"},
			{"app/page/route.go:44:", "generic function page.generic"},
		}},
		{"nofollow", [][]string{
			{"app/page/route.go:24:", "interface method", "reader.Read", "pass the values"},
			{"app/page/route.go:25:", "function value", "fn"},
			{"app/page/route.go:26:", "go statement"},
			{"app/page/route.go:27:", "fmt.Println", "outside this module"},
			{"app/page/route.go:28:", "go statement"},
			{"helpers/helpers.go:16:", "interface method", "sink.Take"},
			{"helpers/helpers.go:22:", "c.Query", "called directly"},
			{"helpers/helpers.go:28:", "variable of type helpers.querier"},
			{"helpers/helpers.go:34:", "app/other does not wrap app/page"},
		}},
		{"typeerror", [][]string{
			{"app/page/route.go:5:17:", "cannot use"},
			{"app/dup/b+/route.go:1:", "app/dup/a+ and app/dup/b+", "parameter directories"},
			{"app/getpage/route.go:7:", "GET"},
		}},
		{"badoutside", [][]string{{"bad/bad.go:3:15:", "cannot use"}}},
		{"bridge", [][]string{{"app/bad/route.go:5:17:", "cannot use"}}},
		{"layoutnopage", [][]string{
			{"app/settings/layout.tsx:1:", `"/settings"`, "app/user+", "no page"},
		}},
		{"rolefiles", [][]string{
			{"app/layout.go:7:", "Load", "func Layout"},
			{"app/layout.go:9:", "POST", "route.go"},
			{"app/layout.go:11:", "SSR", "LayoutSSR"},
			{"app/layout.go:13:", "Cache", "route.go"},
			{"app/mixed/route.go:7:", "Layout", "layout.go"},
			{"app/mixed/route.go:9:", "LayoutSSR", "layout.go"},
			{"app/mixed/extra.go:3:", "Cache", "route.go"},
		}},
		{"structure", [][]string{
			{"app/empty/layout.go:5:", "Path is not declared"},
			{"app/empty/layout.go:7:", "Load", "func Layout"},
			{"app/empty/layout.go:9:", "POST", "layout"},
			{"app/empty/layout.go:1:", "no pages"},
			{"app/both/layout.go:5:", "Layout", "func(c *tifa.Ctx) (T, error)"},
			{"app/_hidden/route.go:1:", "app/_hidden", "rename"},
			{"app/testdata/x/layout.tsx:1:", "app/testdata", "rename"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.app, func(t *testing.T) {
			m, err := analyzeApp(t, tc.app)
			if err == nil {
				t.Fatalf("Analyze succeeded with manifest %+v", m)
			}
			var errs Errors
			if !errors.As(err, &errs) {
				t.Fatalf("error %T is not Errors: %v", err, err)
			}
			if len(errs) != len(tc.want) {
				t.Errorf("got %d errors, want %d:\n%v", len(errs), len(tc.want), err)
			}
			for _, parts := range tc.want {
				if !containsError(errs, parts) {
					t.Errorf("no error contains all of %q in:\n%v", parts, err)
				}
			}
		})
	}
}

func containsError(errs Errors, parts []string) bool {
	for _, e := range errs {
		msg := e.Error()
		ok := true
		for _, p := range parts {
			if !strings.Contains(msg, p) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestAnalyzeDoesNotRunInit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "init-ran")
	t.Setenv("TIFA_TEST_INIT_MARKER", marker)
	m, err := analyzeApp(t, "initwrite")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(m.Routes) != 1 || m.Routes[0].ID != "app/page/route" {
		t.Fatalf("the package with init() was not analyzed: %+v", m.Routes)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init() ran during analysis: stat %s: %v", marker, err)
	}
}

func TestDirSegments(t *testing.T) {
	cases := []struct{ dir, want string }{
		{"app", "/"},
		{"app/about", "/about"},
		{"app/projects/project+", "/projects/{project}"},
		{"app/projects/project+/orders/order+", "/projects/{project}/orders/{order}"},
		{"app/docs/path++", "/docs/{path...}"},
		{"app/v1.2/a-b_c~d", "/v1.2/a-b_c~d"},
		// Parameter names follow ServeMux: any Go identifier, keywords included.
		{"app/kinds/type+/range++", "/kinds/{type}/{range...}"},
	}
	for _, tc := range cases {
		segs, err := dirSegments(tc.dir)
		if err != nil {
			t.Errorf("dirSegments(%q): %v", tc.dir, err)
			continue
		}
		if got := formatPath(segs); got != tc.want {
			t.Errorf("dirSegments(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
	bad := []struct{ dir, want string }{
		{"app/a+b", `"+"`},
		{"app/c++x", `"+"`},
		{"app/1a+", "parameter name"},
		{"app/a-b+", "parameter name"},
		{"app/+", "parameter name"},
		{"app/++", "parameter name"},
		{"app/x+++", "parameter name"},
		{"app/rest++/more", "rest of the path"},
		{"app/id+/x/id+", "twice"},
		{"app/caf\u00e9", "letters"},
		{"app/a b", `" "`},
		{"app/%61", `"%"`},
		{"app/{x}", `"{"`},
		{"app/$x", `"$"`},
	}
	for _, tc := range bad {
		if _, err := dirSegments(tc.dir); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("dirSegments(%q) = %v, want an error containing %s", tc.dir, err, tc.want)
		}
	}
}

// net/http reads GODEBUG=httpmuxgo121 once at startup, so the check runs the badpath case in a
// child test process started with the old ServeMux semantics. The verdict must not change.
func TestConflictsIgnoreGODEBUG(t *testing.T) {
	if os.Getenv("GODEBUG") == "httpmuxgo121=1" {
		t.Skip("already the child process")
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestViolations$/^badpath$", "-test.count=1")
	cmd.Env = append(os.Environ(), "GODEBUG=httpmuxgo121=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("badpath under httpmuxgo121=1: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("child ran no test:\n%s", out)
	}
}

// comparePaths reimplements the ServeMux comparison. Under the default GODEBUG the real ServeMux
// is the reference: it panics on registering exactly the pairs related as equivalent or overlaps.
func TestConflictsMatchServeMux(t *testing.T) {
	if os.Getenv("GODEBUG") == "httpmuxgo121=1" {
		t.Skip("the reference ServeMux runs the old semantics")
	}
	paths := []string{
		"/", "/a", "/b", "/{x}", "/{x...}", "/a/b", "/a/{y}", "/{y}/b", "/a/{y...}", "/{x}/{y}",
		"/a/{x}/c", "/{y}/b/c", "/d/b/{z}", "/a/b/{z...}", "/{x}/b/{z...}", "/files/{p...}", "/files/x",
		// ServeMux unescapes literal segments before comparing them.
		"/%61", "/%2F", "/%2f/{x}", "/{x}/a", "/a%2Fb", "/%zz",
	}
	for i, p1 := range paths {
		for _, p2 := range paths[i+1:] {
			s1, err1 := parsePath(p1)
			s2, err2 := parsePath(p2)
			if err1 != nil || err2 != nil {
				t.Fatalf("parsePath: %v %v", err1, err2)
			}
			want := !muxAccepts(p1, p2)
			rel := comparePaths(muxSegments(s1), muxSegments(s2))
			if got := rel == equivalent || rel == overlaps; got != want {
				t.Errorf("conflicts(%q, %q) = %t, ServeMux says %t", p1, p2, got, want)
			}
		}
	}
}

func muxAccepts(paths ...string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	mux := http.NewServeMux()
	for _, p := range paths {
		if p == "/" {
			p = "/{$}"
		}
		mux.Handle(p, http.NotFoundHandler())
	}
	return true
}

// A Ctx reached through an embedding struct is still a Ctx: its Query calls count.
func TestQueryThroughEmbeddedCtx(t *testing.T) {
	m, err := analyzeApp(t, "embeddedctx")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := m.Routes[0].Load.Query; !reflect.DeepEqual(got, []string{"lang"}) {
		t.Errorf("Query = %v, want [lang]", got)
	}
}

// The TypeScript form follows what encoding/json writes, field selection and method sets included.
func TestLoadResultShape(t *testing.T) {
	m, err := analyzeApp(t, "jsonshape")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	got, problems := tsType(m.Routes[0].Load.Result)
	if len(problems) > 0 {
		t.Fatalf("problems: %+v", problems)
	}
	want := "{ id: string; Kind: string; note?: string; X: string; \"dash-name\": number; quoted: string | null; " +
		"flag: string; text: string; bytes4: number[]; count: string; " +
		"opt?: boolean | null; byNum: Record<string, string> | null; byCode: Record<string, number> | null; " +
		"level: string; num: number; numP: number | null; raw: string | null; list: number[] | null; grid: boolean[]; empty: []; none: []; " +
		"when: string; tree: { name: string; kids: (Node | null)[] | null } | null; " +
		"dur: number; pt: string | null; pts: (string | null)[] | null; st: string; byStatus: Record<string, number> | null; " +
		"tags: string[] | null; o2?: number }"
	if got != want {
		t.Errorf("tsType =\n%s\nwant\n%s", got, want)
	}
}

// go.mod says go 1.26.0 while the framework needs 1.27. go list then fails for the whole module
// and packages.Load returns no packages without an error. Analyze must surface the go command's
// message instead of blaming route.go.
func TestGoListFailureIsReported(t *testing.T) {
	_, err := analyzeApp(t, "stalegomod")
	var errs Errors
	if err == nil || errors.As(err, &errs) {
		t.Fatalf("want the go command's error, got %v", err)
	}
	if !strings.Contains(err.Error(), "go mod tidy") {
		t.Errorf("error lacks the go command's advice: %v", err)
	}
}

// The matcher must reject documents that do not fit, or the shape check proves nothing.
func TestShapeMatcherRejectsMismatches(t *testing.T) {
	shape, err := parseTS("{ a: number; b?: string | null; c: (number | null)[] | null }")
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{
		`{"a":"1","c":null}`, `{"c":null}`, `{"a":1,"c":null,"d":1}`, `{"a":1,"c":["x"]}`, `{"a":1,"b":2,"c":null}`,
	} {
		var v any
		if err := json.Unmarshal([]byte(doc), &v); err != nil {
			t.Fatal(err)
		}
		if shape.match(v, "$", nil) == nil {
			t.Errorf("%s matched", doc)
		}
	}
	var ok any
	json.Unmarshal([]byte(`{"a":1,"c":[1,null]}`), &ok)
	if err := shape.match(ok, "$", nil); err != nil {
		t.Errorf("valid document rejected: %v", err)
	}
}

// Helpers that receive the Ctx are followed, across packages, through two levels, through a
// struct embedding the Ctx and through recursion. Each Load gets what it reaches. A helper that
// only reads c.Context(), like auth.User, adds nothing and passes.
func TestFollowHelpers(t *testing.T) {
	m, err := analyzeApp(t, "follow")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	loads := map[string]*Load{}
	for _, r := range m.Routes {
		loads[r.ID] = r.Load
	}
	list, other := loads["app/items/id+/route"], loads["app/items/other/route"]
	if !reflect.DeepEqual(list.Params, []string{"id"}) ||
		!reflect.DeepEqual(list.Query, []string{"depth", "dir", "page", "sort"}) ||
		!reflect.DeepEqual(list.Uses, []string{"app/layout"}) ||
		!reflect.DeepEqual(list.AllQuery, []string{"depth", "dir", "page", "site", "sort"}) {
		t.Errorf("list Load = %+v", list)
	}
	// c.QueryAll counts as a query parameter, also through a helper, and through tifa.Use of a
	// Layout (list above).
	if len(other.Params) != 0 || !reflect.DeepEqual(other.Query, []string{"dir", "sort", "tag"}) || len(other.Uses) != 0 ||
		!reflect.DeepEqual(other.AllQuery, []string{"dir", "sort", "tag"}) {
		t.Errorf("other Load = %+v", other)
	}
	if len(m.Layouts) != 1 || !reflect.DeepEqual(m.Layouts[0].Load.Query, []string{"site"}) {
		t.Errorf("Layouts = %+v", m.Layouts)
	}
}

// The places the Ctx may go: a method receiver, an argument of a function in the module or in
// tifa, a field of a struct literal that carries it, a new local variable of its own type.
func TestCtxAllowedUses(t *testing.T) {
	m, err := analyzeApp(t, "ctxallowed")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	want := []string{"closure", "inline", "page", "paren", "q", "size", "sort"}
	if got := m.Routes[0].Load.Query; !reflect.DeepEqual(got, want) {
		t.Errorf("Query = %v, want %v", got, want)
	}
}

// An app whose pages are all TSX has no Go package. Its module path still comes from go.mod, and
// tifa build names the executable after it.
func TestModuleOfAppWithoutGoPackages(t *testing.T) {
	m, err := analyzeApp(t, "tsxonly")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if m.Module != "example.com/tsxonly" {
		t.Errorf("Module = %q", m.Module)
	}
}

// The module path is the one the go command loads: a module block is a valid way to write it,
// and -modfile in GOFLAGS replaces go.mod.
func TestModuleIsWhatTheGoCommandLoads(t *testing.T) {
	m, err := analyzeApp(t, "moduleblock")
	if err != nil || m.Module != "example.com/moduleblock" {
		t.Errorf("module block: %+v, %v", m, err)
	}
	modfile, err := filepath.Abs(filepath.Join("testdata", "altmodfile", "alternate.mod"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", "-mod=readonly -modfile="+modfile)
	m, err = analyzeApp(t, "altmodfile")
	if err != nil || m.Module != "example.com/effective" {
		t.Errorf("-modfile: %+v, %v", m, err)
	}
}

// A directory inside a module is not an application root, even though the go command finds the
// module above it.
func TestRootMustHoldGoMod(t *testing.T) {
	_, err := Analyze(filepath.Join("testdata", "tsxonly", "app"))
	if err == nil || !strings.Contains(err.Error(), "not the root of a module") {
		t.Fatalf("got %v", err)
	}
}

// An app that the go.work in use leaves out has its go.mod in place, so the error points at go.work.
func TestModuleLeftOutOfGoWork(t *testing.T) {
	work, err := filepath.Abs(filepath.Join("testdata", "go.work.outside"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", work)
	_, err = analyzeApp(t, "tsxonly")
	if err == nil || !strings.Contains(err.Error(), "go work use") {
		t.Fatalf("got %v", err)
	}
}
