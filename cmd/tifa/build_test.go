package main

import (
	"debug/buildinfo"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/config"
)

func TestMissingDependencyHint(t *testing.T) {
	for _, stderr := range []string{
		"main.go:4:2: missing go.sum entry for module providing package github.com/spf13/cobra",
		"main.go:4:2: no required module provides package github.com/spf13/cobra; to add it:",
		"go: updates to go.mod needed; to update it:\n\tgo mod tidy",
	} {
		if hint := missingDependencyHint(stderr); !strings.Contains(hint, "go get -tool github.com/liustack/tifa/cmd/tifa") {
			t.Errorf("hint for %q = %q", stderr, hint)
		}
	}
	if hint := missingDependencyHint("main.go:1: syntax error"); hint != "" {
		t.Errorf("hint for a syntax error = %q", hint)
	}
}

func TestExecutablesGoToBin(t *testing.T) {
	if got := defaultOutput("/app", "example.com/team/shop"); got != filepath.Join("/app", "bin", "shop") {
		t.Errorf("tifa build writes %s", got)
	}
	if devOutput != "bin" {
		t.Errorf("tifa dev writes into %s", devOutput)
	}
}

// An app with pages bundles TanStack Router, so its version is checked unless the project file
// says to skip it. An app without pages bundles nothing.
func TestBuildChecksTanStackOfAppsWithPages(t *testing.T) {
	root := t.TempDir() // no node_modules: the check fails on the missing router
	if err := checkTanStack(root, config.Build{}, true); err == nil || !strings.Contains(err.Error(), "@tanstack/react-router is not installed") {
		t.Errorf("pages: %v", err)
	}
	if err := checkTanStack(root, config.Build{SkipTanStackCheck: true}, true); err != nil {
		t.Errorf("build.skip_tanstack_check: %v", err)
	}
	if err := checkTanStack(root, config.Build{}, false); err != nil {
		t.Errorf("no pages: %v", err)
	}
}

// A -pgo in GOFLAGS wins over .tifa/default.pgo in tifa build, as for any go build. tifa dev
// passes -pgo=off on the command line, which wins over GOFLAGS, so it never builds with a profile.
func TestDevBuildsWithoutPGOWhateverGOFLAGSSays(t *testing.T) {
	profile, err := filepath.Abs(filepath.Join("..", "..", "internal", "codegen", "engine.pgo"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":            "module example.com/pgo\n\ngo 1.27\n",
		".tifa/main.go":     "package main\n\nfunc main() {}\n",
		".tifa/default.pgo": "not a profile, go build fails if it reads this\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOFLAGS", "-pgo="+profile)
	for _, tc := range []struct {
		dev  bool
		want string
	}{{false, profile}, {true, ""}} {
		exe := filepath.Join(t.TempDir(), "app")
		if printed, err := goBuild(t.Context(), root, exe, tc.dev, io.Discard, io.Discard); err != nil {
			t.Fatalf("dev %t: %v\n%s", tc.dev, err, printed)
		}
		info, err := buildinfo.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		for _, s := range info.Settings {
			if s.Key == "-pgo" {
				got = s.Value
			}
		}
		if got != tc.want {
			t.Errorf("dev %t: built with -pgo=%q, want %q", tc.dev, got, tc.want)
		}
	}
}
