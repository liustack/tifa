package bundle

import (
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestShopBundleRendersInQuickJS bundles the prototype in examples/shop with
// this package and runs the prototype's own Go tests against the result. They
// render orders in QuickJS and check the hydration script. The test works on a
// copy of the shop's Go module and leaves examples/shop untouched.
//
// It needs the generated files of one prototype build and is off by default:
//
//	(cd examples/shop && pnpm install --frozen-lockfile && pnpm build)
//	TIFA_SHOP_E2E=1 go test ./internal/bundle -run TestShopBundle -v
func TestShopBundleRendersInQuickJS(t *testing.T) {
	if os.Getenv("TIFA_SHOP_E2E") == "" {
		t.Skip("set TIFA_SHOP_E2E=1 after pnpm build in examples/shop")
	}
	_, file, _, _ := runtime.Caller(0)
	shop := filepath.Join(filepath.Dir(file), "..", "..", "examples", "shop")
	prototype := filepath.Join(shop, "internal", "ssr", "assets")
	for _, need := range []string{
		filepath.Join(shop, "node_modules", "react-dom", "package.json"),
		filepath.Join(shop, "web", "generated", "routes.ts"),
		filepath.Join(prototype, "build.json"),
	} {
		if _, err := os.Stat(need); err != nil {
			t.Fatalf("run pnpm install --frozen-lockfile && pnpm build in examples/shop first: %v", err)
		}
	}

	// BuildID reproduces the prototype's buildId, which hashes the assets
	// directory before build.json is written.
	var built struct{ BuildID string }
	readJSON(t, filepath.Join(prototype, "build.json"), &built)
	withoutBuildJSON := t.TempDir()
	copyTree(t, prototype, withoutBuildJSON, func(rel string) bool { return rel != "build.json" })
	if id, err := BuildID(withoutBuildJSON); err != nil || id != built.BuildID {
		t.Fatalf("BuildID = %s, %v, prototype buildId %s", id, err, built.BuildID)
	}

	module := t.TempDir()
	copyTree(t, shop, module, func(rel string) bool {
		top := strings.Split(rel, string(filepath.Separator))[0]
		return (rel == "go.mod" || rel == "go.sum" || top == "cmd" || top == "internal") &&
			!strings.HasPrefix(rel, filepath.Join("internal", "ssr", "assets"))
	})
	assets := filepath.Join(module, "internal", "ssr", "assets")
	copyTree(t, prototype, assets, func(rel string) bool { return rel == "routes.json" })

	if _, err := SSR(SSROptions{
		Root:     shop,
		Entry:    "web/server.tsx",
		Outfile:  filepath.Join(assets, "server.js"),
		Tsconfig: "tsconfig.json",
		Write:    true,
	}); err != nil {
		t.Fatal(err)
	}
	client, _, err := Client(ClientOptions{
		Root:     shop,
		Entry:    "web/client.tsx",
		Outdir:   filepath.Join(assets, "public"),
		Tsconfig: "tsconfig.json",
		Write:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := BuildID(assets)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]string{
		"buildId": id,
		"script":  "/assets/" + client.Script,
		"style":   "/assets/" + client.Style,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "build.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := os.ReadFile(filepath.Join(assets, "server.js"))
	if err != nil {
		t.Fatal(err)
	}
	// Same esbuild version and options as the prototype: identical content
	// hashes, so identical file names.
	if ours, theirs := fileNames(t, filepath.Join(assets, "public")), fileNames(t, filepath.Join(prototype, "public")); ours != theirs {
		t.Errorf("browser files differ from the prototype build:\nGo:        %s\nprototype: %s", ours, theirs)
	}
	t.Logf("SSR bundle: %d bytes, sha256 %x", len(server), sha256.Sum256(server))

	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = module
	out, err := cmd.CombinedOutput()
	t.Logf("go test ./... in the shop copy:\n%s", out)
	if err != nil {
		t.Fatal(err)
	}
}

func fileNames(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, " ")
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

// copyTree copies the regular files under src whose relative path keep accepts.
func copyTree(t *testing.T, src, dst string, keep func(rel string) bool) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !keep(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(dst, filepath.Dir(rel)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
