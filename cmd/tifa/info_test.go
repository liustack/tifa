package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// tifa version names the release the command was built from, as it is when go tool tifa builds it
// from the go.mod of an app. A build in a checkout or from a replaced module is a development build.
func TestTifaVersion(t *testing.T) {
	release := &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "v0.6.0", Sum: "h1:abc"}}
	replaced := &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "v0.6.0", Sum: "h1:abc", Replace: &debug.Module{Path: "../tifa"}}}
	checkout := &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "(devel)"}}
	for name, tc := range map[string]struct {
		info *debug.BuildInfo
		want string
	}{
		"release": {release, "v0.6.0"}, "replaced": {replaced, "devel"}, "checkout": {checkout, "devel"}, "none": {nil, "devel"},
	} {
		if got := tifaVersion(tc.info); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestVersionCommandsPrintOneLine(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		out, _, err := run(t, args...)
		if err != nil || out != "tifa "+tifaVersion(readBuildInfo())+"\n" {
			t.Errorf("%v: %q, %v", args, out, err)
		}
	}
}

// fakeTools puts programs that print version in a folder that is all of PATH.
func fakeTools(t *testing.T, versions map[string]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake programs are shell scripts")
	}
	dir := t.TempDir()
	for name, out := range versions {
		script := "#!/bin/sh\necho '" + out + "'\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

// tifa info lists what a bug report needs: the versions of Tifa, Go, Node and pnpm, the system and
// whether the folder is an app with a tifa.yaml. A program that is missing is "not found", not an error.
func TestInfo(t *testing.T) {
	fakeTools(t, map[string]string{"go": "go version go1.26.1 darwin/arm64", "node": "v24.1.0", "pnpm": "10.33.0"})
	app := t.TempDir()
	writeFile(t, filepath.Join(app, "tifa.yaml"), "site:\n  url: https://example.com\n")
	out, _, err := run(t, "info", "--dir", app)
	if err != nil {
		t.Fatal(err)
	}
	want := "Tifa       " + tifaVersion(readBuildInfo()) + "\n" +
		"Go         go1.26.1\n" +
		"Node       v24.1.0\n" +
		"pnpm       10.33.0\n" +
		"System     " + runtime.GOOS + "/" + runtime.GOARCH + "\n" +
		"tifa.yaml  found\n"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}

	out, _, err = run(t, "info", "--json", "--dir", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got["tifa"] != tifaVersion(readBuildInfo()) || got["go"] != "go1.26.1" || got["node"] != "v24.1.0" || got["pnpm"] != "10.33.0" ||
		got["os"] != runtime.GOOS || got["arch"] != runtime.GOARCH || got["tifaYaml"] != false {
		t.Errorf("json %v", got)
	}
}

func TestInfoWithoutNodeAndPnpm(t *testing.T) {
	fakeTools(t, map[string]string{"go": "go version go1.26.1 linux/amd64"})
	out, _, err := run(t, "info", "--dir", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Node       not found\n") || !strings.Contains(out, "pnpm       not found\n") || !strings.Contains(out, "tifa.yaml  not found\n") {
		t.Errorf("got\n%s", out)
	}
	out, _, _ = run(t, "info", "--json", "--dir", t.TempDir())
	if !strings.Contains(out, `"node": "not found"`) || !strings.Contains(out, `"pnpm": "not found"`) {
		t.Errorf("json\n%s", out)
	}
}
