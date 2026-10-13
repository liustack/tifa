package analyze

import (
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
	wantLayouts := []struct {
		id, parent, path, pkg string
		ssr, tsx, load        bool
		routes                []string
	}{
		{"app", "", "/", "example.com/valid/app", true, true, true,
			[]string{"app/api/health", "app/auth/login", "app/auth/register", "app/files/browse", "app/home", "app/projects/export", "app/projects/list", "app/projects/orders/detail"}},
		{"app/auth", "app", "/", "example.com/valid/app/auth", true, true, true, []string{"app/auth/login", "app/auth/register"}},
		{"app/files", "app", "/files", "", true, true, false, []string{"app/files/browse"}},
		{"app/projects", "app", "/projects/{projectId}", "example.com/valid/app/projects", false, true, true,
			[]string{"app/projects/export", "app/projects/list", "app/projects/orders/detail"}},
	}
	for _, w := range wantLayouts {
		l := layouts[w.id]
		if l.Parent != w.parent || l.Path != w.path || l.Package != w.pkg || l.SSR != w.ssr || l.TSX != w.tsx || (l.Load != nil) != w.load {
			t.Errorf("layout %s = %+v, want %+v", w.id, l, w)
		}
		if !reflect.DeepEqual(l.Routes, w.routes) {
			t.Errorf("layout %s routes = %v, want %v", w.id, l.Routes, w.routes)
		}
	}
	if got := layouts["app/projects"].Load; got == nil || got.Type != "example.com/valid/app/projects.Project" ||
		!reflect.DeepEqual(got.Params, []string{"projectId"}) || !reflect.DeepEqual(got.Query, []string{"currency"}) ||
		!reflect.DeepEqual(got.AllQuery, []string{"currency", "lang"}) || !reflect.DeepEqual(got.Uses, []string{"app"}) {
		t.Errorf("projects layout Load = %+v", got)
	}
	if got := layouts["app"].Load; got == nil || !reflect.DeepEqual(got.Query, []string{"lang"}) ||
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
		id, path         string
		page, ssr, cache bool
		methods, layouts []string
	}{
		{"app/home", "/", true, true, false, nil, []string{"app"}},
		{"app/auth/login", "/login", true, true, false, []string{"POST"}, []string{"app", "app/auth"}},
		{"app/auth/register", "/register", true, true, false, nil, []string{"app", "app/auth"}},
		{"app/projects/list", "/projects/{projectId}", true, false, true, nil, []string{"app", "app/projects"}},
		{"app/projects/orders/detail", "/projects/{projectId}/orders/{orderId}", true, false, false, []string{"POST", "DELETE"}, []string{"app", "app/projects"}},
		{"app/files/browse", "/files/{path...}", true, true, false, nil, []string{"app", "app/files"}},
		{"app/api/health", "/api/health", false, true, false, []string{"GET"}, []string{"app"}},
		{"app/projects/export", "/exports/projects", false, false, false, []string{"GET"}, []string{"app", "app/projects"}},
	}
	for _, w := range wantRoutes {
		r := routes[w.id]
		if r.Path != w.path || r.Page != w.page || r.SSR != w.ssr || r.Cache != w.cache ||
			!reflect.DeepEqual(r.Methods, w.methods) || !reflect.DeepEqual(r.Layouts, w.layouts) {
			t.Errorf("route %s = %+v, want %+v", w.id, r, w)
		}
	}

	if r := routes["app/home"]; r.Load == nil || r.Load.Type != "string" || !reflect.DeepEqual(r.Load.Query, []string{"ref"}) ||
		!reflect.DeepEqual(r.Load.AllQuery, []string{"ref"}) {
		t.Errorf("home Load = %+v", r.Load)
	}
	// list reads page and sort itself, and through tifa.Use(c, projects.Load) also depends on
	// currency and, one level further up, lang.
	if r := routes["app/projects/list"]; r.Load == nil || r.Load.Type != "[]string" ||
		!reflect.DeepEqual(r.Load.Query, []string{"page", "sort"}) || len(r.Load.Params) != 0 ||
		!reflect.DeepEqual(r.Load.AllQuery, []string{"currency", "lang", "page", "sort"}) ||
		!reflect.DeepEqual(r.Load.Uses, []string{"app/projects"}) {
		t.Errorf("list Load = %+v", r.Load)
	}
	if r := routes["app/projects/orders/detail"]; r.Load == nil ||
		r.Load.Type != "*example.com/valid/app/projects/orders/detail.Order" ||
		!reflect.DeepEqual(r.Load.Params, []string{"orderId", "projectId"}) {
		t.Errorf("detail Load = %+v", r.Load)
	}
	if r := routes["app/projects/orders/detail"]; r.Package != "example.com/valid/app/projects/orders/detail" {
		t.Errorf("detail Package = %q", r.Package)
	}
	if r := routes["app/auth/register"]; r.Load != nil {
		t.Errorf("register Load = %+v, want nil", r.Load)
	}
}

// Each violation must be reported with its file and line. want lists, per expected error,
// substrings that one single error message must all contain.
func TestViolations(t *testing.T) {
	cases := []struct {
		app  string
		want [][]string
	}{
		{"duppath", [][]string{{"app/b/route.go:3:", `"/items/{key}"`, "app/a/route.go:3"}}},
		{"ongetpage", [][]string{{"app/page/route.go:7:", "OnGet", "route.tsx"}}},
		{"layoutparam", [][]string{{"app/projects/layout.go:6:", `"projectId"`, `"/projects"`}}},
		{"usenonancestor", [][]string{{"app/shop/route.go:11:", "admin.Load", "ancestor"}}},
		{"useclosure", [][]string{
			{"app/page/route.go:12:", "package-level function", "closure"},
			{"app/page/route.go:13:", "package-level function", "method"},
		}},
		{"badtype", [][]string{
			{"app/page/route.go:37:", "Data.Updates", "chan int"},
			{"app/page/route.go:37:", "Data.ByScore", "map[float64]string"},
			{"app/page/route.go:37:", "Data.Items[].Extra", "any"},
			{"app/page/route.go:37:", "Data.Raw", "json.RawMessage", "Marshaler"},
			{"app/page/route.go:37:", "Data.Bytes[]", "Marshaler"},
			{"app/page/route.go:37:", "Data.Ptrs[]", "Marshaler"},
		}},
		{"badpath", [][]string{
			{"app/noslash/route.go:3:", `"projects"`, "start with /"},
			{"app/trailing/route.go:3:", "trailing /"},
			{"app/dupparam/route.go:3:", `"id"`, "twice"},
			{"app/midwild/route.go:3:", "{rest...}", "last"},
			{"app/concat/route.go:3:", "string literal"},
			{"app/dollar/route.go:3:", "$"},
			{"app/overlap2/route.go:3:", `"/a/{b}"`, `"/{a}/b"`},
			{"app/cb/route.go:3:", `"/{y}/b/c"`, `"/a/{x}/c"`},
			{"app/cc/route.go:3:", `"/d/b/{z}"`, `"/{y}/b/c"`},
		}},
		{"paramname", [][]string{
			{"app/item/route.go:9:", `"id"`, `"/items/{key}"`},
			{"app/item/route.go:9:", "Query", "constant"},
		}},
		{"indirect", [][]string{
			{"app/page/route.go:17:", "c.Query", "called directly"},
			{"app/page/route.go:18:", "(*tifa.Ctx).Query", "method expression"},
			{"app/page/route.go:19:", "tifa.Use", "called directly"},
			{"app/page/route.go:20:", "tifa.Use", "called directly"},
			{"app/page/route.go:27:", "c.Param", "called directly"},
		}},
		{"typeerror", [][]string{
			{"app/page/route.go:5:17:", "cannot use"},
			{"app/dup2/route.go:3:", "duplicates"},
			{"app/getpage/route.go:7:", "OnGet"},
		}},
		{"structure", [][]string{
			{"app/orphan/route.tsx:1:", "route.go"},
			{"app/both/layout.go:1:", "route.go", "layout.go"},
			{"app/nopath/route.go:1:", "const Path"},
			{"app/empty/layout.go:5:", "Path", "layout"},
			{"app/empty/layout.go:7:", "Load", "func(c *tifa.Ctx) (T, error)"},
			{"app/empty/layout.go:9:", "OnPost", "layout"},
			{"app/empty/layout.go:1:", "no pages"},
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
	if len(m.Routes) != 1 || m.Routes[0].ID != "app/page" {
		t.Fatalf("the package with init() was not analyzed: %+v", m.Routes)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init() ran during analysis: stat %s: %v", marker, err)
	}
}

func TestCommonPrefix(t *testing.T) {
	cases := []struct {
		paths []string
		want  string
	}{
		{[]string{"/projects/{projectId}", "/projects/{projectId}/orders/{orderId}"}, "/projects/{projectId}"},
		{[]string{"/login", "/register"}, "/"},
		{[]string{"/", "/about"}, "/"},
		{[]string{"/"}, "/"},
		{[]string{"/files/{path...}"}, "/files"},
		{[]string{"/a/{x}/b", "/a/{y}/b"}, "/a"},
		{[]string{"/a/b", "/a/bc"}, "/a"},
	}
	for _, tc := range cases {
		var segs [][]segment
		for _, p := range tc.paths {
			s, err := parsePath(p)
			if err != nil {
				t.Fatalf("parsePath(%q): %v", p, err)
			}
			segs = append(segs, s)
		}
		if got := formatPath(commonPrefix(segs)); got != tc.want {
			t.Errorf("commonPrefix(%q) = %q, want %q", tc.paths, got, tc.want)
		}
	}
}

// Parameter names follow ServeMux: any Go identifier, keywords included.
func TestParsePathAcceptsKeywordParams(t *testing.T) {
	segs, err := parsePath("/kinds/{type}/{range...}")
	if err != nil {
		t.Fatalf("parsePath: %v", err)
	}
	if got := formatPath(segs); got != "/kinds/{type}/{range...}" {
		t.Errorf("formatPath = %q", got)
	}
	for _, bad := range []string{"/{1a}", "/{a-b}", "/{}"} {
		if _, err := parsePath(bad); err == nil {
			t.Errorf("parsePath(%q) succeeded", bad)
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
	want := "{ id: string; Kind: string; note?: string; X: string; Bad: number; count: string; " +
		"opt?: boolean | null; byNum: Record<string, string> | null; byCode: Record<string, number> | null; " +
		"level: string; num: number; raw: string | null; list: number[] | null; grid: boolean[]; " +
		"when: string; stamp: string; notM: { v: number }; value: { v: number }; " +
		"tree: { name: string; kids: (Node | null)[] | null } | null }"
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
