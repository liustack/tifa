package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/js"
)

var (
	// stringLiteral finds the names routes.tsx might read from a module: every identifier the
	// generator spells as a Go string.
	stringLiteral = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)"`)
	moduleImport  = regexp.MustCompile(`import \* as (m\d+) from "([^"]+)"`)
	moduleRead    = regexp.MustCompile(`\b(m\d+)\.(\w+)`)
	standInExport = regexp.MustCompile(`(?m)^export (?:default\b|const (\w+)|\{([^}]*)\})`)
)

// In the browser build, routes.tsx gets a stand-in for each route module it imports statically
// (lazyRouteModule in js/vite/plugin.js), all but the root Layout's. routes.tsx must read nothing
// of such a module that the stand-in does not export. Every module of the test app here exports
// every name the generator spells, so a new export the generator reads fails this test until the
// stand-in exports it too.
func TestHeadModulesReadOnlyWhatTheirStandInExports(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	source, err := os.ReadFile("tsgen.go")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"default", "head"}
	for _, m := range stringLiteral.FindAllSubmatch(source, -1) {
		if name := string(m[1]); !slices.Contains(names, name) && name != "Document" {
			names = append(names, name)
		}
	}
	root := filepath.Join("testdata", "router")
	rootLayout := filepath.Join(root, "app", "layout.tsx")
	exportsOf := func(files []string) (map[string][]string, error) {
		out, err := fakeExports(files)
		for _, f := range files {
			if f != rootLayout {
				out[f] = names
			}
		}
		return out, err
	}
	m, err := analyze.Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(root, m, false, exportsOf)
	if err != nil {
		t.Fatal(err)
	}
	tree := string(files["routes.tsx"])
	standIns := map[string]bool{}
	for _, m := range moduleImport.FindAllStringSubmatch(tree, -1) {
		if m[2] != "../app/layout" {
			standIns[m[1]] = true
		}
	}
	if len(standIns) < 2 {
		t.Fatalf("routes.tsx imports %d route modules statically besides the root Layout, want several:\n%s", len(standIns), tree)
	}
	read := map[string]bool{}
	for _, m := range moduleRead.FindAllStringSubmatch(tree, -1) {
		if standIns[m[1]] {
			read[m[2]] = true
		}
	}

	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "const { lazyRouteModule } = await import(process.argv[1])\n" +
		"process.stdout.write(lazyRouteModule('router', '/app/x/route.tsx'))\n"
	out, err := exec.Command(node, "--input-type=module", "-e", script, plugin).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	exported := map[string]bool{}
	for _, m := range standInExport.FindAllStringSubmatch(string(out), -1) {
		switch {
		case m[1] != "":
			exported[m[1]] = true
		case m[2] != "":
			for _, item := range strings.Split(m[2], ",") {
				fields := strings.Fields(item)
				exported[fields[len(fields)-1]] = true
			}
		default:
			exported["default"] = true
		}
	}
	for name := range read {
		if !exported[name] {
			t.Errorf("routes.tsx reads %s of a route module, which its stand-in does not export:\n%s", name, out)
		}
	}
	if len(read) == 0 {
		t.Errorf("routes.tsx reads nothing of the route modules it imports statically:\n%s", tree)
	}
}
