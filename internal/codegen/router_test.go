package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
)

// TestGeneratedRouterBehaves runs testdata/router/router.test.tsx in Node against the route
// tree generated for testdata/router: server renders through the SSR entry, client navigation
// on a memory history, and the data collector against a stubbed fetch. It needs node and the
// packages of test/apps/pages:
//
//	(cd test/apps/pages && pnpm install --frozen-lockfile)
func TestGeneratedRouterBehaves(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	modules, err := filepath.Abs(filepath.Join("..", "..", "test", "apps", "pages", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modules, "@tanstack", "react-router")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}

	src := filepath.Join("testdata", "router")
	m, err := analyze.Analyze(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(src, m, false)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, files); err != nil {
		t.Fatal(err)
	}
	plugin, err := bundle.Generated(root)
	if err != nil {
		t.Fatal(err)
	}
	// router.test.tsx runs the server renders and the browser routers on Node's build of
	// TanStack, head.test.tsx the head code of the browser on its browser build, call.test.ts the
	// generated handler functions.
	for _, entry := range []struct {
		file     string
		platform api.Platform
	}{{"router.test.tsx", api.PlatformNode}, {"head.test.tsx", api.PlatformBrowser}, {"call.test.ts", api.PlatformNode}} {
		name := strings.TrimSuffix(entry.file, filepath.Ext(entry.file))
		out := filepath.Join(root, name+".mjs")
		result := api.Build(api.BuildOptions{
			AbsWorkingDir: root,
			EntryPoints:   []string{entry.file},
			Outfile:       out,
			Bundle:        true,
			Write:         true,
			Platform:      entry.platform,
			Format:        api.FormatESModule,
			Tsconfig:      filepath.Join(root, Dir, "tsconfig.json"),
			NodePaths:     []string{modules},
			Define:        map[string]string{"process.env.NODE_ENV": `"production"`, "__TIFA_ASSETS__": `{"script":"/assets/app.js","style":"/assets/app.css"}`},
			Loader:        map[string]api.Loader{".css": api.LoaderEmpty},
			Plugins:       []api.Plugin{plugin},
			LogLevel:      api.LogLevelSilent,
		})
		if len(result.Errors) > 0 {
			t.Fatalf("bundle %s: %v", name, result.Errors)
		}
		cmd := exec.Command(node, out)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("node %s: %v\n%s", name, err, output)
		}
		t.Logf("%s:\n%s", name, output)
	}

	// The generated tree, the runtime and the fixture's TSX type-check together.
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	tsconfig := `{ "extends": "./.tifa/tsconfig.json", "include": ["app", "router.test.tsx", "head.test.tsx", "call.test.ts", ".tifa/routes.tsx", ".tifa/server.ts", ".tifa/client.ts", ".tifa/runtime/env.d.ts"] }`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	tsc := exec.Command(node, filepath.Join(modules, "typescript", "bin", "tsc"), "--noEmit", "-p", ".")
	tsc.Dir = root
	if output, err := tsc.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, output)
	}
}
