package bundle

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writePackage writes the package.json of a package with version into dir.
func writePackage(t *testing.T, dir, name, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"name": "` + name + `", "version": "` + version + `"}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// flatTanStack installs the two packages side by side, as npm does.
func flatTanStack(t *testing.T, router, core string) string {
	root := t.TempDir()
	writePackage(t, filepath.Join(root, "node_modules/@tanstack/react-router"), "@tanstack/react-router", router)
	writePackage(t, filepath.Join(root, "node_modules/@tanstack/router-core"), "@tanstack/router-core", core)
	return root
}

// pnpmTanStack installs them the way pnpm does: the app sees a link to react-router, which finds
// router-core next to its real directory. A hoisted router-core of another version sits in the
// app's node_modules, which react-router does not resolve.
func pnpmTanStack(t *testing.T, router, core string) string {
	root := t.TempDir()
	store := filepath.Join(root, "node_modules/.pnpm/@tanstack+react-router@"+router+"/node_modules/@tanstack")
	writePackage(t, filepath.Join(store, "react-router"), "@tanstack/react-router", router)
	writePackage(t, filepath.Join(store, "router-core"), "@tanstack/router-core", core)
	symlink(t, filepath.Join(store, "react-router"), filepath.Join(root, "node_modules/@tanstack/react-router"))
	writePackage(t, filepath.Join(root, "node_modules/@tanstack/router-core"), "@tanstack/router-core", "0.0.1")
	return root
}

func TestCheckTanStackPassesTheTestedVersions(t *testing.T) {
	nested := t.TempDir()
	writePackage(t, filepath.Join(nested, "node_modules/@tanstack/react-router"), "@tanstack/react-router", TestedReactRouter)
	writePackage(t, filepath.Join(nested, "node_modules/@tanstack/react-router/node_modules/@tanstack/router-core"), "@tanstack/router-core", TestedRouterCore)
	writePackage(t, filepath.Join(nested, "node_modules/@tanstack/router-core"), "@tanstack/router-core", "0.0.1")
	// A workspace installs the packages in a folder above the app.
	workspace := flatTanStack(t, TestedReactRouter, TestedRouterCore)
	app := filepath.Join(workspace, "apps", "shop")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, root := range map[string]string{
		"flat":      flatTanStack(t, TestedReactRouter, TestedRouterCore),
		"pnpm":      pnpmTanStack(t, TestedReactRouter, TestedRouterCore),
		"nested":    nested,
		"workspace": app,
	} {
		if err := CheckTanStack(root); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Node and Vite look the packages up from the real directory of the app. An app reached through
// a link finds the packages installed above its real directory, not above the link.
func TestCheckTanStackLooksUpFromTheRealAppDirectory(t *testing.T) {
	workspace := flatTanStack(t, TestedReactRouter, TestedRouterCore)
	app := filepath.Join(workspace, "apps", "shop")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "shop")
	symlink(t, app, link)
	if err := CheckTanStack(link); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTanStackRefusesOtherVersions(t *testing.T) {
	cases := map[string]string{
		"react-router": flatTanStack(t, "1.171.0", TestedRouterCore),
		"router-core":  pnpmTanStack(t, TestedReactRouter, "1.172.1"),
	}
	for name, root := range cases {
		err := CheckTanStack(root)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		msg := err.Error()
		for _, want := range []string{
			// what is installed and what tifa is tested with
			"@tanstack/react-router " + map[string]string{"react-router": "1.171.0", "router-core": TestedReactRouter}[name],
			"@tanstack/router-core " + map[string]string{"react-router": TestedRouterCore, "router-core": "1.172.1"}[name],
			"tested with @tanstack/react-router " + TestedReactRouter + " and @tanstack/router-core " + TestedRouterCore,
			// the risk and the way out
			"serializationAdapters", "blank",
			`"@tanstack/react-router": "` + TestedReactRouter + `"`,
			"build.skip_tanstack_check: true",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: the error lacks %q:\n%s", name, want, msg)
			}
		}
	}
}

func TestCheckTanStackNeedsTheRouterInstalled(t *testing.T) {
	err := CheckTanStack(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "@tanstack/react-router is not installed") {
		t.Fatalf("got %v", err)
	}
	root := t.TempDir()
	writePackage(t, filepath.Join(root, "node_modules/@tanstack/react-router"), "@tanstack/react-router", TestedReactRouter)
	if err := CheckTanStack(root); err == nil || !strings.Contains(err.Error(), "@tanstack/router-core is not installed") {
		t.Fatalf("without router-core: %v", err)
	}
}

// The apps of this repository install the versions tifa is tested with, so the tests that bundle
// them test those versions. After changing TestedReactRouter, install it in each.
func TestAppsOfTheRepositoryUseTheTestedTanStack(t *testing.T) {
	pinned := regexp.MustCompile(`"@tanstack/react-router": "([^"]+)"`)
	locked := regexp.MustCompile(`(?m)^  '(@tanstack/(?:react-router|router-core))@([0-9][^'(]*)['(]`)
	for _, app := range []string{"../../examples/shop", "../../test/apps/pages", "../../test/apps/tailwind"} {
		pkg, err := os.ReadFile(filepath.Join(app, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		if m := pinned.FindSubmatch(pkg); m == nil || string(m[1]) != TestedReactRouter {
			t.Errorf("%s/package.json pins @tanstack/react-router %q, want %s", app, m, TestedReactRouter)
		}
		lock, err := os.ReadFile(filepath.Join(app, "pnpm-lock.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"@tanstack/react-router": TestedReactRouter, "@tanstack/router-core": TestedRouterCore}
		found := map[string]bool{}
		for _, m := range locked.FindAllSubmatch(lock, -1) {
			name, version := string(m[1]), string(m[2])
			found[name] = true
			if version != want[name] {
				t.Errorf("%s/pnpm-lock.yaml locks %s %s, want %s", app, name, version, want[name])
			}
		}
		if len(found) != 2 {
			t.Errorf("%s/pnpm-lock.yaml locks %v, want both packages", app, found)
		}
	}
}
