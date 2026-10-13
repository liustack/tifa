package codegen

import (
	"bytes"
	"flag"
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
	m := &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{{ID: "app/page", Path: "/page", Page: true, SSR: true}}}
	_, err := Generate(root, m, false)
	if err == nil || !strings.Contains(err.Error(), "app/page/route.tsx") {
		t.Fatalf("got %v", err)
	}
}

func TestRootLayoutCannotReadPathParameters(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "app/page/route.tsx"), "export default function Page() { return null }\n")
	m := &analyze.Manifest{
		Module:  "example.com/x",
		Layouts: []analyze.Layout{{ID: "app", Path: "/{tenant}", Load: &analyze.Load{Params: []string{"tenant"}}, SSR: true, Routes: []string{"app/page"}}},
		Routes:  []analyze.Route{{ID: "app/page", Path: "/{tenant}", Page: true, SSR: true, Layouts: []string{"app"}}},
	}
	_, err := Generate(root, m, false)
	if err == nil || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("got %v", err)
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
	m := &analyze.Manifest{Module: "example.com/x", Routes: []analyze.Route{{ID: "app/api", Path: "/api", Methods: []string{"GET"}, SSR: true}}}
	files, err := Generate(t.TempDir(), m, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, []string{"main.go", "routes.go"}) {
		t.Errorf("generated %q", got)
	}
}
