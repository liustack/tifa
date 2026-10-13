package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ./+route of a TSX file under app/ is the generated .tifa/app/<dir>/+route.ts, and tifa/client
// is the runtime module, the way the generated tsconfig tells TypeScript.
func TestGeneratedPluginResolvesRouteModulesAndTheRuntime(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"app/shop/layout.tsx":            "import { route } from './+route'\nimport { clientOnly } from 'tifa/client'\nexport const out = [route, clientOnly]\n",
		"app/shop/items/route.tsx":       "import { route } from './+route'\nimport { route as shop } from '../+route'\nexport const out = [route, shop]\n",
		".tifa/app/shop/+route.ts":       "export const route = 'shop-route-module'\n",
		".tifa/app/shop/items/+route.ts": "export const route = 'items-route-module'\n",
		".tifa/runtime/client.ts":        "export const clientOnly = 'runtime-client-module'\n",
	})
	for entry, want := range map[string][]string{
		"app/shop/layout.tsx":      {"shop-route-module", "runtime-client-module"},
		"app/shop/items/route.tsx": {"items-route-module", "shop-route-module"},
	} {
		plugin, err := Generated(root)
		if err != nil {
			t.Fatal(err)
		}
		result := api.Build(api.BuildOptions{
			AbsWorkingDir: root,
			EntryPoints:   []string{entry},
			Bundle:        true,
			Format:        api.FormatESModule,
			Plugins:       []api.Plugin{plugin},
		})
		if len(result.Errors) > 0 {
			t.Fatalf("%s: %v", entry, result.Errors)
		}
		out := string(result.OutputFiles[0].Contents)
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: bundle lacks %s:\n%s", entry, w, out)
			}
		}
	}
}

func TestGeneratedPluginRejectsRouteModulesOutsideApp(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"lib/x.ts": "import { route } from './+route'\nexport const out = route\n"})
	plugin, err := Generated(root)
	if err != nil {
		t.Fatal(err)
	}
	result := api.Build(api.BuildOptions{
		AbsWorkingDir: root,
		EntryPoints:   []string{"lib/x.ts"},
		Bundle:        true,
		Plugins:       []api.Plugin{plugin},
	})
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Text, "app/") {
		t.Fatalf("errors: %v", result.Errors)
	}
}

func TestGeneratedPluginAcceptsARelativeRoot(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"app/page/route.tsx":       "import { route } from './+route'\nexport const out = route\n",
		".tifa/app/page/+route.ts": "export const route = 'page-route-module'\n",
	})
	t.Chdir(root)
	plugin, err := Generated(".")
	if err != nil {
		t.Fatal(err)
	}
	result := api.Build(api.BuildOptions{AbsWorkingDir: root, EntryPoints: []string{"app/page/route.tsx"}, Bundle: true, Plugins: []api.Plugin{plugin}})
	if len(result.Errors) > 0 {
		t.Fatalf("errors: %v", result.Errors)
	}
}
