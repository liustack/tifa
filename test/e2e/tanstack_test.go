package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/bundle"
)

// A copy of test/apps/pages whose @tanstack/react-router says it is 1.171.0 fails to build with
// the versions in the error, before Vite runs. With build.skip_tanstack_check it builds.
func TestBuildChecksTheTanStackVersion(t *testing.T) {
	skipUnlessEnabled(t)
	dir := copyApp(t, "pages")
	fakeReactRouter(t, dir, "1.171.0")
	cmd := exec.Command(cli(t), "build", "--dir", dir, "-o", filepath.Join(t.TempDir(), "pages"))
	out, err := cmd.CombinedOutput()
	for _, want := range []string{
		"@tanstack/react-router 1.171.0 with @tanstack/router-core " + bundle.TestedRouterCore,
		"tested with @tanstack/react-router " + bundle.TestedReactRouter,
		"build.skip_tanstack_check: true",
	} {
		if err == nil || !strings.Contains(string(out), want) {
			t.Errorf("tifa build: %v, the output lacks %q:\n%s", err, want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".tifa", "dist", "ssr.js")); err == nil {
		t.Error("Vite ran although the check failed")
	}
	if err := os.WriteFile(filepath.Join(dir, "tifa.yaml"), []byte("build:\n  skip_tanstack_check: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buildAt(t, dir)
}

// tifa dev checks the version too: the first build fails with the error in the page, and adding
// build.skip_tanstack_check to tifa.yaml while it runs rebuilds and serves the page.
func TestDevChecksTheTanStackVersion(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("tifa dev needs interrupts, which Windows lacks")
	}
	dir := copyApp(t, "pages")
	fakeReactRouter(t, dir, "1.171.0")
	d := startDev(t, dir)
	d.waitLog(t, 0, buildFailed)
	want := "@tanstack/react-router 1.171.0 with @tanstack/router-core " + bundle.TestedRouterCore
	failed := d.waitStatus(t, func(st devStatus) bool { return st.Error != "" })
	if failed.Title != "Build failed" || !strings.Contains(failed.Error, want) || !strings.Contains(failed.Error, "build.skip_tanstack_check: true") {
		t.Fatalf("status %+v", failed)
	}
	if status, _, body := get(t, d.base+"/"); status != 502 || !strings.Contains(body, want) {
		t.Fatalf("GET /: %d, want 502 with %q:\n%.2000s", status, want, body)
	}
	mark := len(d.log.String())
	if err := os.WriteFile(filepath.Join(dir, "tifa.yaml"), []byte("build:\n  skip_tanstack_check: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.waitLog(t, mark, ready)
	d.waitStatus(t, func(st devStatus) bool { return st.Version != "" && st.Error == "" })
	expectBody(t, d.base+"/", "North shop")
}

// fakeReactRouter replaces the node_modules link of the app copy at dir with a folder of links
// to the original packages, except that @tanstack/react-router is a copy of its package.json with
// version, next to links to its other files. Its router-core is linked where Node finds it from
// there.
func fakeReactRouter(t *testing.T, dir, version string) {
	t.Helper()
	modules := filepath.Join(dir, "node_modules")
	original, err := filepath.EvalSymlinks(modules)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(modules); err != nil {
		t.Fatal(err)
	}
	linkEntries(t, original, modules, "@tanstack")
	linkEntries(t, filepath.Join(original, "@tanstack"), filepath.Join(modules, "@tanstack"), "react-router")
	router, err := filepath.EvalSymlinks(filepath.Join(original, "@tanstack", "react-router"))
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(modules, "@tanstack", "react-router")
	linkEntries(t, router, fake, "package.json")
	data, err := os.ReadFile(filepath.Join(router, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned := `"version": "` + bundle.TestedReactRouter + `"`
	if !strings.Contains(string(data), pinned) {
		t.Fatalf("%s/package.json lacks %s", router, pinned)
	}
	data = []byte(strings.Replace(string(data), pinned, `"version": "`+version+`"`, 1))
	if err := os.WriteFile(filepath.Join(fake, "package.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(filepath.Dir(router), "router-core")
	if err := os.Symlink(core, filepath.Join(modules, "@tanstack", "router-core")); err != nil {
		t.Fatal(err)
	}
}

// linkEntries makes dst a folder that links to every entry of src but skip.
func linkEntries(t *testing.T, src, dst, skip string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == skip {
			continue
		}
		if err := os.Symlink(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

// The apps of this repository install the versions tifa is tested with, so the tests that bundle
// them test those versions. After changing bundle.TestedReactRouter, install it in each.
func TestAppsOfTheRepositoryUseTheTestedTanStack(t *testing.T) {
	pinned := regexp.MustCompile(`"@tanstack/react-router": "([^"]+)"`)
	locked := regexp.MustCompile(`(?m)^  '(@tanstack/(?:react-router|router-core))@([0-9][^'(]*)['(]`)
	for _, app := range []string{"../../examples/shop", "../../test/apps/pages", "../../test/apps/tailwind", "../../test/apps/mui"} {
		pkg, err := os.ReadFile(filepath.Join(app, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		if m := pinned.FindSubmatch(pkg); m == nil || string(m[1]) != bundle.TestedReactRouter {
			t.Errorf("%s/package.json pins @tanstack/react-router %q, want %s", app, m, bundle.TestedReactRouter)
		}
		lock, err := os.ReadFile(filepath.Join(app, "pnpm-lock.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"@tanstack/react-router": bundle.TestedReactRouter, "@tanstack/router-core": bundle.TestedRouterCore}
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
