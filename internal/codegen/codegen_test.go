package codegen

import (
	"bytes"
	"flag"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/analyze"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

var (
	exportedName    = regexp.MustCompile(`(?m)^export (?:async )?(?:const|let|function|class) (\w+)`)
	exportedDefault = regexp.MustCompile(`(?m)^export (?:default |\{ \w+ as default \})`)
)

// fakeExports reads exports the way the fixtures write them, without Node. bundle.Node.Exports
// is tested on its own and in TestGeneratedRouterBehaves.
func fakeExports(files []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, m := range exportedName.FindAllSubmatch(data, -1) {
			names = append(names, string(m[1]))
		}
		if exportedDefault.Match(data) {
			names = append(names, "default")
		}
		slices.Sort(names)
		out[f] = names
	}
	return out, nil
}

func generate(t *testing.T, app string, dev bool) map[string][]byte {
	t.Helper()
	root := filepath.Join("testdata", app)
	m, err := analyze.Analyze(root)
	if err != nil {
		t.Fatalf("Analyze %s: %v", app, err)
	}
	files, err := Generate(root, m, dev, fakeExports)
	if err != nil {
		t.Fatalf("Generate %s: %v", app, err)
	}
	return files
}

// The generated files of each test application must match testdata/golden/<app>. The runtime and
// the Vite plugin are copied from the js package as they are and are left out. Run go test -update to rewrite them.
func TestGolden(t *testing.T) {
	for _, app := range []string{"shop", "bare"} {
		t.Run(app, func(t *testing.T) {
			files := generate(t, app, false)
			golden := filepath.Join("testdata", "golden", app)
			for _, name := range slices.Sorted(maps.Keys(files)) {
				if strings.HasPrefix(name, "runtime/") || strings.HasPrefix(name, "vite/") {
					continue
				}
				path := filepath.Join(golden, filepath.FromSlash(name)+".golden")
				if *update {
					writeFile(t, path, string(files[name]))
					continue
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("%s: %v (run go test -update)", name, err)
					continue
				}
				if !bytes.Equal(files[name], want) {
					t.Errorf("%s differs from %s:\n%s", name, path, files[name])
				}
			}
			// A golden file without a generated counterpart means a file is no longer generated.
			err := filepath.WalkDir(golden, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(golden, path)
				name := strings.TrimSuffix(filepath.ToSlash(rel), ".golden")
				if _, ok := files[name]; !ok {
					if *update {
						return os.Remove(path)
					}
					t.Errorf("%s is no longer generated", name)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	first := generate(t, "shop", false)
	for range 3 {
		again := generate(t, "shop", false)
		if !maps.EqualFunc(first, again, bytes.Equal) {
			t.Fatal("two runs generated different files")
		}
	}
}

func TestGenerateWritesTheRuntime(t *testing.T) {
	files := generate(t, "shop", false)
	for _, name := range []string{"runtime/router.tsx", "runtime/data.ts", "runtime/client.ts", "runtime/host.js", "runtime/host.d.ts"} {
		if len(files[name]) == 0 {
			t.Errorf("%s is missing", name)
		}
	}
	if _, ok := files["runtime/dev-client.js"]; ok {
		t.Error("a production build carries the dev client")
	}
}

func TestDevClientEntryLoadsTheRefreshClient(t *testing.T) {
	files := generate(t, "shop", true)
	if !strings.Contains(string(files["client.ts"]), `import "./runtime/dev-client.js"`) {
		t.Errorf("dev client entry:\n%s", files["client.ts"])
	}
	if len(files["runtime/dev-client.js"]) == 0 {
		t.Error("dev build lacks runtime/dev-client.js")
	}
}

// The main of tifa dev calls server.DevMain, which lets tifa dev reload the SSR bundle in the
// running app. The main of tifa build calls server.Main (see the golden files).
func TestDevMainCallsDevMain(t *testing.T) {
	files := generate(t, "shop", true)
	if !strings.Contains(string(files["main.go"]), "server.DevMain(server.App{") {
		t.Errorf("dev main.go:\n%s", files["main.go"])
	}
	if prod := generate(t, "shop", false); strings.Contains(string(prod["main.go"]), "DevMain") {
		t.Errorf("production main.go:\n%s", prod["main.go"])
	}
}

func TestPageWithoutDefaultExportFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app/page/page.tsx"), "export function Page() { return null }\n")
	m := &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{{ID: "app/page/route", Dir: "app/page", Path: "/page", Page: true, SSR: true}}}
	_, err := Generate(root, m, false, fakeExports)
	if err == nil || !strings.Contains(err.Error(), "app/page/page.tsx") {
		t.Fatalf("got %v", err)
	}
}

// The server writes the document around the React root, so a root Layout that still exports a
// Document component fails the build instead of being left out silently.
func TestRootLayoutDocumentFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app/layout.tsx"), "export default function A() { return null }\nexport function Document() { return null }\n")
	writeFile(t, filepath.Join(root, "app/page/page.tsx"), "export default function P() { return null }\n")
	m := &analyze.Manifest{
		Module:  "example.com/x",
		Layouts: []analyze.Layout{{ID: "app/layout", Dir: "app", Path: "/", TSX: true, SSR: true, Routes: []string{"app/page/route"}}},
		Routes:  []analyze.Route{{ID: "app/page/route", Dir: "app/page", Path: "/page", Page: true, SSR: true, Layouts: []string{"app/layout"}}},
	}
	_, err := Generate(root, m, false, fakeExports)
	if err == nil || !strings.Contains(err.Error(), "htmlAttributes") {
		t.Fatalf("got %v", err)
	}
}

// The entries pass the root Layout's htmlAttributes to the runtime, which writes them on <html>.
func TestRootLayoutHTMLAttributesReachTheEntries(t *testing.T) {
	files := generate(t, "router", false)
	if !strings.Contains(string(files["routes.tsx"]), "export const htmlAttributes = m1.htmlAttributes\n") {
		t.Errorf("routes.tsx does not export the root Layout's htmlAttributes:\n%s", files["routes.tsx"])
	}
	for name, call := range map[string]string{"server.ts": "serve(routeTree, htmlAttributes)", "client.ts": "hydrate(routeTree, htmlAttributes)"} {
		if !strings.Contains(string(files[name]), call) {
			t.Errorf("%s lacks %s:\n%s", name, call, files[name])
		}
	}
}

// A component remounts when a path parameter its Load reads changes, itself or through a Layout
// it uses. A Layout that reads no parameter keeps its state.
func TestRemountFollowsTheParametersLoadReads(t *testing.T) {
	root := t.TempDir()
	const project = "app/projects/projectId+"
	for _, f := range []string{"app/layout.tsx", project + "/layout.tsx", project + "/page.tsx", project + "/orders/orderId+/page.tsx", project + "/files/path++/page.tsx"} {
		writeFile(t, filepath.Join(root, f), "export default function C() { return null }\n")
	}
	str := types.Typ[types.String]
	scope := []string{project + "/route", project + "/orders/orderId+/route", project + "/files/path++/route"}
	m := &analyze.Manifest{
		Module: "example.com/x",
		Layouts: []analyze.Layout{
			{ID: "app/layout", Dir: "app", Path: "/", TSX: true, Load: &analyze.Load{Result: str}, SSR: true, Routes: scope},
			{ID: project + "/layout", Dir: project, Parent: "app/layout", Path: "/projects/{projectId}", TSX: true, Load: &analyze.Load{Params: []string{"projectId"}, Result: str}, SSR: true, Routes: scope},
		},
		Routes: []analyze.Route{
			{ID: project + "/route", Dir: project, Path: "/projects/{projectId}", Page: true, SSR: true, Layouts: []string{"app/layout", project + "/layout"}, Load: &analyze.Load{Uses: []string{project + "/layout"}, Result: str}},
			{ID: project + "/orders/orderId+/route", Dir: project + "/orders/orderId+", Path: "/projects/{projectId}/orders/{orderId}", Page: true, SSR: true, Layouts: []string{"app/layout", project + "/layout"}, Load: &analyze.Load{Params: []string{"orderId"}, Result: str}},
			{ID: project + "/files/path++/route", Dir: project + "/files/path++", Path: "/projects/{projectId}/files/{path...}", Page: true, SSR: true, Layouts: []string{"app/layout", project + "/layout"}, Load: &analyze.Load{Params: []string{"path"}, Result: str}},
		},
	}
	files, err := Generate(root, m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	tree := string(files["routes.tsx"])
	for _, want := range []string{
		`load<D3>(context, "app/projects/projectId+/route", location),
  staleTime: Infinity,
  preloadStaleTime: Infinity,
  remountDeps: ({ params }) => [params.projectId],`,
		`remountDeps: ({ params }) => [params.orderId],`,
		`remountDeps: ({ params }) => [params._splat],`,
		`load<D2>(context, "app/projects/projectId+/layout", location),
  staleTime: Infinity,
  preloadStaleTime: Infinity,
  remountDeps: ({ params }) => [params.projectId],`,
	} {
		if !strings.Contains(tree, want) {
			t.Errorf("routes.tsx lacks\n%s", want)
		}
	}
	if app := tree[strings.Index(tree, `load<D1>`):strings.Index(tree, `load<D2>`)]; strings.Contains(app, "remountDeps") {
		t.Errorf("the root Layout reads no parameter but remounts:\n%s", app)
	}
}

// A Layout and a page in one directory are two routes: the Layout is a path route, and the page
// is its index route, so the one route.gen module of the directory names each one.
func TestLayoutAndPageInOneDirectory(t *testing.T) {
	root := t.TempDir()
	const project = "app/projects/projectId+"
	for _, f := range []string{"app/layout.tsx", project + "/layout.tsx", project + "/page.tsx", project + "/orders/orderId+/page.tsx"} {
		writeFile(t, filepath.Join(root, f), "export default function C() { return null }\n")
	}
	scope := []string{project + "/route", project + "/orders/orderId+/route"}
	layouts := []string{"app/layout", project + "/layout"}
	m := &analyze.Manifest{
		Module: "example.com/x",
		Layouts: []analyze.Layout{
			{ID: "app/layout", Dir: "app", Path: "/", TSX: true, SSR: true, Routes: scope},
			{ID: project + "/layout", Dir: project, Parent: "app/layout", Path: "/projects/{projectId}", TSX: true, SSR: true, Routes: scope},
		},
		Routes: []analyze.Route{
			{ID: project + "/route", Dir: project, Path: "/projects/{projectId}", Page: true, SSR: true, Layouts: layouts},
			{ID: project + "/orders/orderId+/route", Dir: project + "/orders/orderId+", Path: "/projects/{projectId}/orders/{orderId}", Page: true, SSR: true, Layouts: layouts},
		},
	}
	files, err := Generate(root, m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	for name, wants := range map[string][]string{
		"app/route.gen.ts":                        {`export const layout = getRouteApi("/_app")`},
		project + "/route.gen.ts":                 {`export const layout = getRouteApi("/_app/projects/$projectId")`, `export const page = getRouteApi("/_app/projects/$projectId/")`},
		project + "/orders/orderId+/route.gen.ts": {`export const page = getRouteApi("/_app/projects/$projectId/orders/$orderId")`},
	} {
		for _, want := range wants {
			if !strings.Contains(string(files[name]), want) {
				t.Errorf("%s lacks %s:\n%s", name, want, files[name])
			}
		}
	}
}

// A Layout in layout_.tsx has no parent, so its route hangs from the framework's root, outside
// the root Layout, and its module is layout_.tsx. It fails into the framework's Error, as the
// root Layout does. The root Layout's catch-all route turns down its URLs, which the router
// matches in part to show its own 404.
func TestReplaceLayoutHangsFromTheFrameworksRoot(t *testing.T) {
	root := t.TempDir()
	const store = "app/store/lang+"
	for _, f := range []string{"app/layout.tsx", "app/page.tsx", store + "/layout_.tsx", store + "/page.tsx", store + "/cart/layout.tsx", store + "/cart/page.tsx"} {
		writeFile(t, filepath.Join(root, f), "export default function C() { return null }\nexport function Error() { return null }\n")
	}
	m := &analyze.Manifest{
		Module: "example.com/x",
		Layouts: []analyze.Layout{
			{ID: "app/layout", Dir: "app", Path: "/", TSX: true, SSR: true, Routes: []string{"app/route"}},
			{ID: store + "/layout", Dir: store, Path: "/store/{lang}", TSX: true, Replace: true, SSR: true, Routes: []string{store + "/route", store + "/cart/route"}},
			{ID: store + "/cart/layout", Dir: store + "/cart", Parent: store + "/layout", Path: "/store/{lang}/cart", TSX: true, SSR: true, Routes: []string{store + "/cart/route"}},
		},
		Routes: []analyze.Route{
			{ID: "app/route", Dir: "app", Path: "/", Page: true, SSR: true, Layouts: []string{"app/layout"}},
			{ID: store + "/route", Dir: store, Path: "/store/{lang}", Page: true, SSR: true, Layouts: []string{store + "/layout"}},
			{ID: store + "/cart/route", Dir: store + "/cart", Path: "/store/{lang}/cart", Page: true, SSR: true, Layouts: []string{store + "/layout", store + "/cart/layout"}},
		},
	}
	files, err := Generate(root, m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	for name, wants := range map[string][]string{
		"routes.tsx": {
			`const m2 = () => import("../app/store/lang+/layout_")`,
			"getParentRoute: () => root,\n  path: \"store/$lang\",\n  component: lazyRouteComponent(m2),\n  errorComponent: DefaultError,\n  notFoundComponent: n2,",
			`params: { parse: outside([["store", null], ["store", null, "cart"]]) }`,
		},
		store + "/route.gen.ts":      {`export const layout = getRouteApi("/store/$lang")`, `export const page = getRouteApi("/store/$lang/")`},
		store + "/cart/route.gen.ts": {`export const layout = getRouteApi("/store/$lang/cart")`, `export const page = getRouteApi("/store/$lang/cart/")`},
	} {
		for _, want := range wants {
			if !strings.Contains(string(files[name]), want) {
				t.Errorf("%s lacks %s:\n%s", name, want, files[name])
			}
		}
	}
}

// Every route shows the first Error going up from it, a Layout from the level above its own, so
// a Layout's Error shows in its Outlet with the Layout around it. A Layout's notFoundComponent
// shows the first Error from the Layout up for a URL that matches nothing below it, and so does
// the index route below a Layout without a page of its own.
func TestErrorBoundaries(t *testing.T) {
	tree := string(generate(t, "shop", false)["routes.tsx"])
	for _, want := range []string{
		// The root Layout fails: nothing is above it but the framework's Error. A URL that
		// matches no page below it shows its own Error in its Outlet.
		"id: \"_app\",\n  component: m1.default,\n  errorComponent: DefaultError,\n  notFoundComponent: n1,",
		"const e1 = m1.Error\n",
		"const n1 = missing(e1)\n",
		// The project Layout fails: the root Layout's Error shows in its place.
		"path: \"projects/$projectId\",\n  component: lazyRouteComponent(m3),\n  errorComponent: e1,\n  notFoundComponent: n3,",
		"const n3 = missing(e3)\n",
		// A page shows its own Error, or else the one of its Layout.
		"path: \"/\",\n  component: lazyRouteComponent(m7),\n  errorComponent: e3,",
		"path: \"orders/$orderId\",\n  component: lazyRouteComponent(m8),\n  errorComponent: e8,",
		"const e8 = lazyRouteComponent(m8, \"Error\")\n",
		// /auth has a Layout but no page: the index route below it shows the root Layout's Error.
		"path: \"auth\",\n  component: lazyRouteComponent(m2),\n  errorComponent: e1,\n  notFoundComponent: n1,",
		"path: \"/\",\n  errorComponent: e1,\n  loader: unmatched,",
		// A URL that no route below the root Layout starts shows the 404 inside the root Layout.
		"path: \"$\",\n  params: { parse: outside([[\"auth\"], [\"projects\", null], [\"auth\", \"login\"], [\"auth\", \"register\"], [\"projects\", null, \"orders\", null]]) },\n  errorComponent: e1,\n  loader: unmatched,",
		"const n0 = missing(DefaultError)\n",
	} {
		if !strings.Contains(tree, want) {
			t.Errorf("routes.tsx lacks %q:\n%s", want, tree)
		}
	}
	if strings.Contains(tree, "NotFound") || strings.Contains(tree, "beforeLoad") {
		t.Errorf("the route tree still has a NotFound or beforeLoad:\n%s", tree)
	}
}

// The browser build hands routes.tsx a lazy stand-in for each module it imports statically, but
// the root Layout's, and TanStack wraps a route whose errorComponent can preload in Suspense. The
// SSR bundle has the component itself, so a route whose Error comes from such a module turns that
// Suspense off, or the browser would hydrate another tree than the server rendered.
func TestStaticErrorsWrapNoSuspense(t *testing.T) {
	tree := string(generate(t, "router", false)["routes.tsx"])
	team := regexp.MustCompile(`import \* as (m\d+) from "\.\./app/teams/team\+/layout"`).FindStringSubmatch(tree)
	root := regexp.MustCompile(`import \* as (m\d+) from "\.\./app/layout"`).FindStringSubmatch(tree)
	if team == nil || root == nil {
		t.Fatalf("routes.tsx imports the team Layout or the root Layout lazily:\n%s", tree)
	}
	errorVar := func(module string) string {
		m := regexp.MustCompile(`const (e\d+) = ` + module + `\.Error\n`).FindStringSubmatch(tree)
		if m == nil {
			t.Fatalf("routes.tsx names no Error of %s:\n%s", module, tree)
		}
		return m[1]
	}
	for _, c := range []struct {
		error string
		off   bool
	}{{errorVar(team[1]), true}, {errorVar(root[1]), false}, {"DefaultError", false}} {
		uses := strings.Count(tree, "errorComponent: "+c.error+",\n")
		off := strings.Count(tree, "errorComponent: "+c.error+",\n  wrapInSuspense: false,\n")
		if uses == 0 || c.off && off != uses || !c.off && off != 0 {
			t.Errorf("%s: %d routes, %d of them without Suspense:\n%s", c.error, uses, off, tree)
		}
	}
}

// NotFound is no export of page.tsx or layout.tsx: a 404 reaches Error as a RouteError with
// status 404, so a module that still exports NotFound fails the build instead of being left out
// silently.
func TestNotFoundExportFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app/layout.tsx"), "export default function A() { return null }\n")
	writeFile(t, filepath.Join(root, "app/page/page.tsx"), "export default function P() { return null }\nexport function NotFound() { return null }\n")
	m := &analyze.Manifest{
		Module:  "example.com/x",
		Layouts: []analyze.Layout{{ID: "app/layout", Dir: "app", Path: "/", TSX: true, SSR: true, Routes: []string{"app/page/route"}}},
		Routes:  []analyze.Route{{ID: "app/page/route", Dir: "app/page", Path: "/page", Page: true, SSR: true, Layouts: []string{"app/layout"}}},
	}
	_, err := Generate(root, m, false, fakeExports)
	if err == nil || !strings.Contains(err.Error(), "app/page/page.tsx") || !strings.Contains(err.Error(), "status === 404") {
		t.Fatalf("got %v", err)
	}
}

func TestLiteralSegmentsMatchLikeServeMux(t *testing.T) {
	for in, want := range map[string]string{"%61": "a", `x"y`: "x%22y", `x\y`: "x%5Cy", "100%25": "100%25", "%zz": "%25zz", "中 文": "中 文", "{id}": "$id", "{rest...}": "$"} {
		segs := segments("/" + in)
		got, err := routerSegment(segs[0])
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v, want %q", in, got, err, want)
		}
	}
	// An escaped brace is a literal, never a parameter, and has no safe spelling in the router.
	for _, in := range []string{"a:b", "%40me", "a%2Fb", "%3F", "%7Bx%7D", "%7B", "%7Brest...%7D", "%24x"} {
		if got, err := routerSegment(segments("/" + in)[0]); err == nil {
			t.Errorf("%s: got %q, want an error", in, got)
		}
	}
}

func TestProjectConfigIsEmbedded(t *testing.T) {
	files := generate(t, "shop", false)
	if string(files["tifa.yaml"]) != "server:\n  addr: 127.0.0.1:8090\n" {
		t.Errorf("tifa.yaml copy: %q", files["tifa.yaml"])
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tifa.yaml"), "")
	writeFile(t, filepath.Join(root, "tifa.json"), "{}")
	if _, err := Generate(root, &analyze.Manifest{Module: "example.com/x"}, false, fakeExports); err == nil {
		t.Error("Generate accepted tifa.yaml next to tifa.json")
	}
	bad := t.TempDir()
	writeFile(t, filepath.Join(bad, "tifa.yaml"), "server:\n  port: 80\n")
	if _, err := Generate(bad, &analyze.Manifest{Module: "example.com/x"}, false, fakeExports); err == nil || !strings.Contains(err.Error(), "server.port") {
		t.Errorf("invalid tifa.yaml: %v", err)
	}
}

func TestWriteKeepsUnchangedFilesAndRemovesStaleOnes(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, map[string][]byte{"main.go": []byte("a"), "app/x/route.gen.ts": []byte("b"), "app/y/route.gen.ts": []byte("c")}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, Dir, "dist/build.json"), "{}")
	keep := filepath.Join(root, Dir, "main.go")
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(keep, past, past); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, map[string][]byte{"main.go": []byte("a"), "app/x/route.gen.ts": []byte("B")}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(keep); err != nil || !info.ModTime().Equal(past) {
		t.Error("an unchanged file was rewritten")
	}
	if data, _ := os.ReadFile(filepath.Join(root, Dir, "app/x/route.gen.ts")); string(data) != "B" {
		t.Errorf("changed file holds %q", data)
	}
	if _, err := os.Stat(filepath.Join(root, Dir, "dist/build.json")); err != nil {
		t.Errorf("Write touched the bundle output: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, Dir, "app/y")); !os.IsNotExist(err) {
		t.Errorf("stale directory still exists: %v", err)
	}
}

func TestInterfaceOnlyAppHasNoTypeScript(t *testing.T) {
	m := &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{{ID: "app/api/route", Dir: "app/api", Package: "example.com/x/app/api", Path: "/api", Handlers: []analyze.Handler{{Method: "GET"}}, SSR: true}}}
	// Without pages nothing reads exports, so such an application builds without Node.
	noNode := func([]string) (map[string][]string, error) {
		t.Error("exports read for an application without pages")
		return nil, nil
	}
	files, err := Generate(t.TempDir(), m, false, noNode)
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, []string{"main.go", "routes.go"}) {
		t.Errorf("generated %q", got)
	}
}

func TestProjectSettingsReadsTheServerSection(t *testing.T) {
	root := t.TempDir()
	cfg, err := ProjectSettings(root)
	if err != nil || cfg.Server.ShutdownTimeout != 10*time.Second {
		t.Fatalf("defaults: %v %v", cfg.Server.ShutdownTimeout, err)
	}
	if err := os.WriteFile(filepath.Join(root, "tifa.yaml"), []byte("server:\n  shutdown_timeout: 3s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, err = ProjectSettings(root); err != nil || cfg.Server.ShutdownTimeout != 3*time.Second {
		t.Fatalf("tifa.yaml: %v %v", cfg.Server.ShutdownTimeout, err)
	}
}

// build.target becomes the target of the tifa plugin in .tifa/vite.js. A wrong one fails early.
func TestBuildTargetReachesVite(t *testing.T) {
	m := &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{{ID: "app/page/route", Dir: "app/page", Path: "/page", Page: true, SSR: true}}}
	for target, want := range map[string]string{
		"":                           `tifa({ target: ["es2020"], i18nextCache: true })`, // the default
		"es2020":                     `tifa({ target: ["es2020"], i18nextCache: true })`,
		" ES2019, chrome90,safari14": `tifa({ target: ["es2019","chrome90","safari14"], i18nextCache: true })`,
	} {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "app/page/page.tsx"), "export default function P() { return null }\n")
		if target != "" {
			writeFile(t, filepath.Join(root, "tifa.yaml"), "build:\n  target: \""+target+"\"\n")
		}
		files, err := Generate(root, m, false, fakeExports)
		if err != nil {
			t.Fatalf("%q: %v", target, err)
		}
		if !strings.Contains(string(files["vite.js"]), want) {
			t.Errorf("%q: vite.js lacks %s:\n%s", target, want, files["vite.js"])
		}
	}
	for _, target := range []string{"es2020,es2022", "chrome", "node18", "es2020;chrome90"} {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "tifa.yaml"), "build:\n  target: \""+target+"\"\n")
		if _, err := Generate(root, m, false, fakeExports); err == nil || !strings.Contains(err.Error(), "build.target") {
			t.Errorf("%q: err = %v, want a build.target error", target, err)
		}
	}
}

// build.i18next_cache turns on the i18next cache of the tifa plugin in .tifa/vite.js, by default.
// Turned off, vite.js does not name the setting, and the plugin leaves i18next without the cache.
func TestBuildI18nextCacheReachesVite(t *testing.T) {
	m := &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{{ID: "app/page/route", Dir: "app/page", Path: "/page", Page: true, SSR: true}}}
	for config, want := range map[string]string{
		"":                                 `tifa({ target: ["es2020"], i18nextCache: true })`,
		"build:\n  i18next_cache: true\n":  `tifa({ target: ["es2020"], i18nextCache: true })`,
		"build:\n  i18next_cache: false\n": `tifa({ target: ["es2020"] })`,
	} {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "app/page/page.tsx"), "export default function P() { return null }\n")
		if config != "" {
			writeFile(t, filepath.Join(root, "tifa.yaml"), config)
		}
		files, err := Generate(root, m, false, fakeExports)
		if err != nil {
			t.Fatalf("%q: %v", config, err)
		}
		if !strings.Contains(string(files["vite.js"]), want) {
			t.Errorf("%q: vite.js lacks %s:\n%s", config, want, files["vite.js"])
		}
	}
}

// A directory gets one route.gen module for what it has: a Layout and an API route in one
// directory share it, and it exports no page, PageData or the old +route names.
func TestRouteGenHoldsLayoutAndHandlersOfOneDirectory(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"app/layout.tsx", "app/page.tsx"} {
		writeFile(t, filepath.Join(root, f), "export default function C() { return null }\n")
	}
	str := types.Typ[types.String]
	scope := []string{"app/route", "app/api/route"}
	m := &analyze.Manifest{
		Module:  "example.com/x",
		Layouts: []analyze.Layout{{ID: "app/layout", Dir: "app", Path: "/", TSX: true, Load: &analyze.Load{Result: str}, SSR: true, Routes: scope}},
		Routes: []analyze.Route{
			{ID: "app/route", Dir: "app", Path: "/", Page: true, SSR: true, Layouts: []string{"app/layout"}, Handlers: []analyze.Handler{{Method: "POST"}}},
		},
	}
	// The API route of the root directory: a page with a POST has the module of the page.
	files, err := Generate(root, m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	mod := string(files["app/route.gen.ts"])
	for _, want := range []string{
		"export type LayoutData = string",
		"export type PageData = undefined",
		"export type Params = Record<string, never>",
		`export const layout = getRouteApi("/_app")`,
		`export const page = getRouteApi("/_app/")`,
		"export async function POST(options: CallOptions = {})",
	} {
		if !strings.Contains(mod, want) {
			t.Errorf("app/route.gen.ts lacks %s:\n%s", want, mod)
		}
	}
	for name := range files {
		if strings.Contains(name, "+route") || strings.Contains(name, "+layout") {
			t.Errorf("generated %s", name)
		}
	}

	// An API route next to a Layout without a page there: layout and handlers, no page.
	writeFile(t, filepath.Join(root, "app/desk/layout.tsx"), "export default function C() { return null }\n")
	m.Layouts = append(m.Layouts, analyze.Layout{ID: "app/desk/layout", Dir: "app/desk", Parent: "app/layout", Path: "/desk", TSX: true, SSR: true, Routes: []string{"app/desk/route", "app/desk/inbox/route"}})
	writeFile(t, filepath.Join(root, "app/desk/inbox/page.tsx"), "export default function C() { return null }\n")
	m.Routes = append(m.Routes,
		analyze.Route{ID: "app/desk/route", Dir: "app/desk", Path: "/desk", SSR: true, Layouts: []string{"app/layout", "app/desk/layout"}, Handlers: []analyze.Handler{{Method: "GET"}}},
		analyze.Route{ID: "app/desk/inbox/route", Dir: "app/desk/inbox", Path: "/desk/inbox", Page: true, SSR: true, Layouts: []string{"app/layout", "app/desk/layout"}})
	files, err = Generate(root, m, false, fakeExports)
	if err != nil {
		t.Fatal(err)
	}
	desk := string(files["app/desk/route.gen.ts"])
	for _, want := range []string{"export type LayoutData = undefined", "export const layout = getRouteApi(", "export async function GET("} {
		if !strings.Contains(desk, want) {
			t.Errorf("app/desk/route.gen.ts lacks %s:\n%s", want, desk)
		}
	}
	for _, not := range []string{"PageData", "export const page"} {
		if strings.Contains(desk, not) {
			t.Errorf("app/desk/route.gen.ts has %s:\n%s", not, desk)
		}
	}
	if inbox := string(files["app/desk/inbox/route.gen.ts"]); !strings.Contains(inbox, "export const page = ") || strings.Contains(inbox, "export const layout") {
		t.Errorf("app/desk/inbox/route.gen.ts:\n%s", inbox)
	}
}
func TestPublicMayNotHoldTifaPaths(t *testing.T) {
	m, err := analyze.Analyze(filepath.Join("testdata", "bare"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "public", "__tifa", "vite", "client.js"), "x")
	_, err = Generate(root, m, false, fakeExports)
	if err == nil || !strings.Contains(err.Error(), "public/__tifa/vite/client.js") || !strings.Contains(err.Error(), "/__tifa/") {
		t.Fatalf("Generate = %v, want an error naming public/__tifa/vite/client.js", err)
	}
	// Other files in public and an empty __tifa folder are fine.
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "public", "robots.txt"), "x")
	if err := os.MkdirAll(filepath.Join(other, "public", "__tifa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkPublic(other, "public"); err != nil {
		t.Fatalf("checkPublic: %v", err)
	}
}
