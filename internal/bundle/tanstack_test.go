package bundle

import (
	"os"
	"path/filepath"
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

// TestTanStackCopiesAreUnchanged hashes the pieces of TanStack source that tifa copies, in the
// packages of test/apps/pages, and fails with the tifa function to compare again when one changed.
// Run pnpm install --frozen-lockfile in test/apps/pages first.
func TestTanStackCopiesAreUnchanged(t *testing.T) {
	app, err := filepath.Abs(filepath.Join("..", "..", "test", "apps", "pages"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(app, "node_modules", "@tanstack", "react-router")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}
	if err := CheckTanStack(app); err != nil {
		t.Fatalf("test/apps/pages does not install the tested versions: %v", err)
	}
	changed, err := CheckCopies(app)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range changed {
		t.Error(message)
	}
}

// A piece that TanStack moved or renamed is reported, not hashed as nothing.
func TestTanStackCopyNeedsItsStartAndEnd(t *testing.T) {
	c := Copy{File: "a.ts", Package: "p", Start: "function f(", End: "}"}
	if _, err := c.Hash("function g() {\n}\n"); err == nil {
		t.Error("a source without the start has a hash")
	}
	if _, err := c.Hash("function f() {\n  1\n"); err == nil {
		t.Error("a source without the end has a hash")
	}
	a, _ := c.Hash("x\nfunction f() {\n  1\n}\ny\n")
	b, _ := c.Hash("z\r\nfunction f() {\r\n  1\r\n}\r\nw\r\n")
	if a == "" || a != b {
		t.Errorf("the hash follows what is outside the piece or the line endings: %q %q", a, b)
	}
	d, _ := c.Hash("function f() {\n  2\n}\n")
	if d == a {
		t.Error("the hash ignores the inside of the piece")
	}
}
