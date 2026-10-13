package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

func clientOptions(root string) ClientOptions {
	return ClientOptions{
		Root:     root,
		Entry:    "entry.ts",
		Outdir:   "public",
		Tsconfig: filepath.Join(root, "tsconfig.json"),
	}
}

func TestClientSplitsLazyImportsIntoHashedFiles(t *testing.T) {
	root := app(t, "client", false)
	assets, result, err := Client(clientOptions(root))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^app-[A-Z0-9]{8}\.js$`).MatchString(assets.Script) {
		t.Errorf("script = %q, want app-<hash>.js", assets.Script)
	}
	if !regexp.MustCompile(`^app-[A-Z0-9]{8}\.css$`).MatchString(assets.Style) {
		t.Errorf("style = %q, want app-<hash>.css", assets.Style)
	}
	files := map[string]string{}
	for _, f := range result.OutputFiles {
		if filepath.Base(filepath.Dir(f.Path)) != "public" {
			t.Fatalf("output outside the output directory: %s", f.Path)
		}
		files[filepath.Base(f.Path)] = string(f.Contents)
	}
	var chunks int
	for name, contents := range files {
		if regexp.MustCompile(`^chunk-[A-Z0-9]{8}\.js$`).MatchString(name) {
			chunks++
			if !strings.Contains(contents, `"production"`) {
				t.Errorf("lazy chunk %s does not inline NODE_ENV: %s", name, contents)
			}
		}
	}
	if chunks == 0 {
		t.Fatalf("lazy import not split into a chunk: %v", keys(files))
	}
	script, ok := files[assets.Script]
	if !ok {
		t.Fatalf("entry %s not among outputs %v", assets.Script, keys(files))
	}
	if !strings.Contains(script, "import(") {
		t.Errorf("entry is not an ES module with a dynamic import: %s", script)
	}
	if _, err := os.Stat(filepath.Join(root, "public")); !os.IsNotExist(err) {
		t.Errorf("Write=false still wrote output: %v", err)
	}
}

func TestClientWritesOutdir(t *testing.T) {
	root := app(t, "client", false)
	o := clientOptions(root)
	o.Write = true
	assets, _, err := Client(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{assets.Script, assets.Style} {
		if _, err := os.Stat(filepath.Join(root, "public", name)); err != nil {
			t.Error(err)
		}
	}
}

func TestClientFindsEntryResolvedByEsbuild(t *testing.T) {
	redirect := api.Plugin{Name: "redirect", Setup: func(b api.PluginBuild) {
		b.OnResolve(api.OnResolveOptions{Filter: `^app-entry$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			return api.OnResolveResult{Path: filepath.Join(args.ResolveDir, "entry.ts")}, nil
		})
	}}
	cases := map[string]struct {
		entry   string
		plugins []api.Plugin
	}{
		"extension omitted":    {entry: "./entry"},
		"redirected by plugin": {entry: "app-entry", plugins: []api.Plugin{redirect}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := app(t, "client", false)
			o := clientOptions(root)
			o.Entry, o.Plugins = c.entry, c.plugins
			assets, _, err := Client(o)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(assets.Script, "app-") || !strings.HasPrefix(assets.Style, "app-") {
				t.Fatalf("assets = %+v, want the app entry and its CSS", assets)
			}
		})
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBuildIDHashesFilesInPathOrder(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"server.js":        "server",
		"routes.json":      "routes",
		"public/app-1.js":  "app",
		"public/app-1.css": "css",
	})
	sum := sha256.Sum256([]byte("css" + "app" + "routes" + "server"))
	want := hex.EncodeToString(sum[:])[:16]
	got, err := BuildID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("BuildID = %s, want %s", got, want)
	}
	writeFiles(t, dir, map[string]string{"public/app-1.js": "app2"})
	changed, err := BuildID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed == got {
		t.Fatal("BuildID did not change with file contents")
	}
}
