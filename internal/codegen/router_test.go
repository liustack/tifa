package codegen

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
)

// routerBuild bundles the tests with Vite for Node to run. router.test.tsx runs the server
// renders and the browser routers on Node's build of TanStack, head.test.tsx the head code of the
// browser on its browser build, call.test.ts the generated handler functions.
const routerBuild = `import { build } from 'vite'
import { generated } from './.tifa/vite/plugin.js'
const assets = '<link rel="stylesheet" href="/assets/app.css"><script type="module" src="/assets/app.js"></script>'
for (const [entry, platform] of [['router.test.tsx', 'node'], ['head.test.tsx', 'browser'], ['call.test.ts', 'node']]) {
  await build({
    configFile: false,
    logLevel: 'warn',
    plugins: [generated()],
    define: { 'process.env.NODE_ENV': '"production"', __TIFA_ASSETS__: JSON.stringify(assets) },
    environments: { ssr: { resolve: { noExternal: true, conditions: [platform, 'import', 'module', 'default'] } } },
    build: {
      ssr: entry,
      outDir: 'out',
      emptyOutDir: false,
      minify: false,
      rolldownOptions: { output: { format: 'es', entryFileNames: entry.replace(/\.tsx?$/, '.mjs') } },
    },
  })
}
`

// TestGeneratedRouterBehaves runs testdata/router/router.test.tsx in Node against the route
// tree generated for testdata/router: server renders through the SSR entry, client navigation
// on a memory history, and the data collector against a stubbed fetch. It needs node and the
// packages of test/apps/pages, Vite among them:
//
//	(cd test/apps/pages && pnpm install --frozen-lockfile)
func TestGeneratedRouterBehaves(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	pages, err := filepath.Abs(filepath.Join("..", "..", "test", "apps", "pages"))
	if err != nil {
		t.Fatal(err)
	}
	modules := filepath.Join(pages, "node_modules")
	if _, err := os.Stat(filepath.Join(modules, "vite")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}

	src := filepath.Join("testdata", "router")
	m, err := analyze.Analyze(src)
	if err != nil {
		t.Fatal(err)
	}
	front := bundle.NewNode(pages, io.Discard)
	defer front.Close()
	files, err := Generate(src, m, false, front.Exports)
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
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.mjs"), []byte(routerBuild), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(node, args...)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("node %v: %v\n%s", args, err, output)
		}
		t.Logf("%v:\n%s", args, output)
	}
	run("build.mjs")
	for _, name := range []string{"router.test", "head.test", "call.test"} {
		run(filepath.Join("out", name+".mjs"))
	}

	// The generated tree, the runtime and the fixture's TSX type-check together.
	tsconfig := `{ "extends": "./.tifa/tsconfig.json", "compilerOptions": { "paths": { "@/*": ["./*"], "tifa/client": ["./.tifa/runtime/client.ts"] } }, "include":["app", "router.test.tsx", "head.test.tsx", "call.test.ts", ".tifa/routes.tsx", ".tifa/server.ts", ".tifa/client.ts", ".tifa/runtime/env.d.ts"] }`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	tsc := exec.Command(node, filepath.Join(modules, "typescript", "bin", "tsc"), "--noEmit", "-p", ".")
	tsc.Dir = root
	if output, err := tsc.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, output)
	}
}
