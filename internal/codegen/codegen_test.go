package codegen

import (
	"bytes"
	"flag"
	"go/types"
	"maps"
	"os"
	"path/filepath"
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

func generate(t *testing.T, app string, dev bool) map[string][]byte {
	t.Helper()
	root := filepath.Join("testdata", app)
	m, err := analyze.Analyze(root)
	if err != nil {
		t.Fatalf("Analyze %s: %v", app, err)
	}
	files, err := Generate(root, m, dev)
	if err != nil {
		t.Fatalf("Generate %s: %v", app, err)
	}
	return files
}

// The generated files of each test application must match testdata/golden/<app>. The runtime is
// copied from the js package as it is and is left out. Run go test -update to rewrite them.
func TestGolden(t *testing.T) {
	for _, app := range []string{"shop", "bare"} {
		t.Run(app, func(t *testing.T) {
			files := generate(t, app, false)
			golden := filepath.Join("testdata", "golden", app)
			for _, name := range slices.Sorted(maps.Keys(files)) {
				if strings.HasPrefix(name, "runtime/") {
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

func TestPageWithoutDefaultExportFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app/page/route.tsx"), "export function Page() { return null }\n")
	m := &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{{ID: "app/page/route", Dir: "app/page", Path: "/page", Page: true, SSR: true}}}
	_, err := Generate(root, m, false)
	if err == nil || !strings.Contains(err.Error(), "app/page/route.tsx") {
		t.Fatalf("got %v", err)
	}
}

// The server writes the document around the React root, so a root Layout that still exports a
// Document component fails the build instead of being left out silently.
func TestRootLayoutDocumentFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app/layout.tsx"), "export default function A() { return null }\nexport function Document() { return null }\n")
	writeFile(t, filepath.Join(root, "app/page/route.tsx"), "export default function P() { return null }\n")
	m := &analyze.Manifest{
		Module:  "example.com/x",
		Layouts: []analyze.Layout{{ID: "app/layout", Dir: "app", Path: "/", TSX: true, SSR: true, Routes: []string{"app/page/route"}}},
		Routes:  []analyze.Route{{ID: "app/page/route", Dir: "app/page", Path: "/page", Page: true, SSR: true, Layouts: []string{"app/layout"}}},
	}
	_, err := Generate(root, m, false)
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
	for _, f := range []string{"app/layout.tsx", project + "/layout.tsx", project + "/route.tsx", project + "/orders/orderId+/route.tsx", project + "/files/path++/route.tsx"} {
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
	files, err := Generate(root, m, false)
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
// is its index route, so the page's +route module and the Layout's +layout module name each one.
func TestLayoutAndPageInOneDirectory(t *testing.T) {
	root := t.TempDir()
	const project = "app/projects/projectId+"
	for _, f := range []string{"app/layout.tsx", project + "/layout.tsx", project + "/route.tsx", project + "/orders/orderId+/route.tsx"} {
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
	files, err := Generate(root, m, false)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"app/+layout.ts":                       `getRouteApi("/_app")`,
		project + "/+layout.ts":                `getRouteApi("/_app/projects/$projectId")`,
		project + "/+route.ts":                 `getRouteApi("/_app/projects/$projectId/")`,
		project + "/orders/orderId+/+route.ts": `getRouteApi("/_app/projects/$projectId/orders/$orderId")`,
	} {
		if !strings.Contains(string(files[name]), want) {
			t.Errorf("%s lacks %s:\n%s", name, want, files[name])
		}
	}
}

// A Layout's NotFound needs no route of its own: the routes right below the Layout carry it, so
// a 404 under the Layout shows it in the Layout's Outlet, and every loader throws a plain
// notFound() that the router takes up from its own route.
func TestLayoutNotFoundAddsNoRoute(t *testing.T) {
	tree := string(generate(t, "router", false)["routes.tsx"])
	if strings.Contains(tree, "_notFound") {
		t.Errorf("the route tree has a NotFound boundary route:\n%s", tree)
	}
	if strings.Contains(tree, "location, \"") {
		t.Errorf("a loader names where its 404 is shown:\n%s", tree)
	}
	for _, want := range []string{
		"path: \"a\",\n  component: lazyRouteComponent(m2),\n  errorComponent: DefaultError,\n  notFoundComponent: m1.NotFound,",
	} {
		if !strings.Contains(tree, want) {
			t.Errorf("routes.tsx lacks %q:\n%s", want, tree)
		}
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
	if _, err := Generate(root, &analyze.Manifest{Module: "example.com/x"}, false); err == nil {
		t.Error("Generate accepted tifa.yaml next to tifa.json")
	}
	bad := t.TempDir()
	writeFile(t, filepath.Join(bad, "tifa.yaml"), "server:\n  port: 80\n")
	if _, err := Generate(bad, &analyze.Manifest{Module: "example.com/x"}, false); err == nil || !strings.Contains(err.Error(), "server.port") {
		t.Errorf("invalid tifa.yaml: %v", err)
	}
}

func TestWriteKeepsUnchangedFilesAndRemovesStaleOnes(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, map[string][]byte{"main.go": []byte("a"), "app/x/+route.ts": []byte("b"), "app/y/+route.ts": []byte("c")}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, Dir, "dist/build.json"), "{}")
	keep := filepath.Join(root, Dir, "main.go")
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(keep, past, past); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, map[string][]byte{"main.go": []byte("a"), "app/x/+route.ts": []byte("B")}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(keep); err != nil || !info.ModTime().Equal(past) {
		t.Error("an unchanged file was rewritten")
	}
	if data, _ := os.ReadFile(filepath.Join(root, Dir, "app/x/+route.ts")); string(data) != "B" {
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
	files, err := Generate(t.TempDir(), m, false)
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
