package main

import (
	"bytes"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/bundle"
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

// An app with pages bundles React DOM, whose string renderer the Vite plugin rewrites, so its
// version is checked. The app tifa new writes installs the minimum version.
func TestBuildChecksReactOfAppsWithPages(t *testing.T) {
	root := t.TempDir() // no node_modules: the check fails on the missing react
	if err := checkReact(root, true); err == nil || !strings.Contains(err.Error(), "react is not installed") {
		t.Errorf("pages: %v", err)
	}
	if err := checkReact(root, false); err != nil {
		t.Errorf("no pages: %v", err)
	}
	pkg, err := os.ReadFile(filepath.Join("newapp", "package.json.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"react", "react-dom"} {
		if want := `"` + name + `": "` + bundle.MinReact + `"`; !strings.Contains(string(pkg), want) {
			t.Errorf("tifa new does not write %s", want)
		}
	}
	// The types follow the minor version of React, not its patches.
	minor := bundle.MinReact[:strings.LastIndex(bundle.MinReact, ".")]
	for _, name := range []string{"@types/react", "@types/react-dom"} {
		if want := `"` + name + `": "` + minor + `.`; !strings.Contains(string(pkg), want) {
			t.Errorf("tifa new does not write %s of React %s", name, minor)
		}
	}
}

// A -pgo in GOFLAGS wins over .tifa/default.pgo in tifa build, as for any go build. tifa dev
// passes -pgo=off on the command line, which wins over GOFLAGS, so it never builds with a profile.
func TestDevBuildsWithoutPGOWhateverGOFLAGSSays(t *testing.T) {
	profile, err := filepath.Abs(filepath.Join("..", "..", "internal", "codegen", "engine.pgo"))
	if err != nil {
		t.Fatal(err)
	}
	root := writeGoApp(t, map[string]string{
		".tifa/main.go":     "package main\n\nfunc main() {}\n",
		".tifa/default.pgo": "not a profile, go build fails if it reads this\n",
	})
	t.Setenv("GOFLAGS", "-pgo="+profile)
	build, err := buildFlags(t.Context(), root, config.Build{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		dev  bool
		want string
	}{{false, profile}, {true, ""}} {
		exe := filepath.Join(t.TempDir(), "app")
		flags := build
		if tc.dev {
			flags = devFlags
		}
		if printed, err := goBuild(t.Context(), root, exe, flags, io.Discard, io.Discard); err != nil {
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

// writeGoApp writes files, given by slash-separated paths, into a new app root with a go.mod.
func writeGoApp(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module example.com/app\n\ngo 1.27\n"
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// tifa build adds -w to the -ldflags of GOFLAGS, and adds nothing when they already say what to
// do with the DWARF debug info (-w or -s), or when they are for a package pattern, which tifa
// cannot match against the main package.
func TestLdflagsAddWToThoseOfGOFLAGS(t *testing.T) {
	for _, tc := range []struct {
		goflags string
		want    []string
	}{
		{"", []string{"-ldflags=-w"}},
		{"-mod=mod -trimpath", []string{"-ldflags=-w"}},
		{"-ldflags=", []string{"-ldflags=-w"}},
		{"-ldflags=-X=main.version=v1", []string{"-ldflags=-X=main.version=v1 -w"}},
		{"--ldflags=-X=main.version=v1", []string{"-ldflags=-X=main.version=v1 -w"}},
		{"'-ldflags= -X=main.version=v1'", []string{"-ldflags=-X=main.version=v1 -w"}},
		{"'-ldflags=-X main.version=v1'", []string{"-ldflags=-X main.version=v1 -w"}},
		{`"-ldflags=-X 'main.version=v 1'"`, []string{"-ldflags=-X 'main.version=v 1' -w"}},
		// As in the go command, the last -ldflags wins.
		{"-ldflags=-s -ldflags=-X=main.version=v1", []string{"-ldflags=-X=main.version=v1 -w"}},
		{"-ldflags=-w", nil},
		{"-ldflags=-s", nil},
		{"'-ldflags=-X main.version=v1 --s=true'", nil},
		{"-ldflags=-w=0", nil},
		{"-ldflags=-w=false", nil},
		{"-ldflags=all=-X=main.version=v1", nil},
		{"-ldflags=-X=main.version=v1 -ldflags=example.com/...=-X=main.version=v2", nil},
	} {
		got, err := ldflags(tc.goflags)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("GOFLAGS %s: %q, %v, want %q", tc.goflags, got, err, tc.want)
		}
	}
	if _, err := ldflags("'-ldflags=-w"); err == nil || !strings.Contains(err.Error(), "GOFLAGS") {
		t.Errorf("an unterminated quote: %v", err)
	}
}

// GOFLAGS can also be in the file go env -w writes, which the go command reads as well.
func TestBuildFlagsReadTheGoEnvFile(t *testing.T) {
	goenv := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(goenv, []byte("GOFLAGS=-ldflags=-X=main.version=v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOENV", goenv)
	t.Setenv("GOFLAGS", "")
	got, err := buildFlags(t.Context(), t.TempDir(), config.Build{})
	if want := []string{"-ldflags=-X=main.version=v1 -w"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("%q, %v, want %q", got, err, want)
	}
}

// With build.debug_info, tifa build passes no flags and does not run go env: it gets there with
// no go command on PATH.
func TestBuildFlagsWithDebugInfoRunNoGoEnv(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got, err := buildFlags(t.Context(), t.TempDir(), config.Build{DebugInfo: true}); err != nil || got != nil {
		t.Errorf("%q, %v, want none", got, err)
	}
}

// The program tifa build makes has no DWARF debug info unless build.debug_info keeps it, and keeps
// the -ldflags of GOFLAGS. Its panics still give the file and line of each frame, which come from
// the line table the runtime keeps in every Go program. tifa dev keeps the debug info.
func TestBuildLeavesOutTheDebugInfo(t *testing.T) {
	root := writeGoApp(t, map[string]string{
		".tifa/main.go": `package main

import "os"

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		panic("boom")
	}
	os.Stdout.WriteString(version)
}
`,
	})
	t.Setenv("GOFLAGS", "-ldflags=-X=main.version=v1")
	for _, tc := range []struct {
		name      string
		dev       bool
		debugInfo bool
	}{{"build", false, false}, {"build.debug_info", false, true}, {"dev", true, false}} {
		flags := devFlags
		if !tc.dev {
			var err error
			if flags, err = buildFlags(t.Context(), root, config.Build{DebugInfo: tc.debugInfo}); err != nil {
				t.Fatal(err)
			}
		}
		exe := filepath.Join(t.TempDir(), "app")
		if printed, err := goBuild(t.Context(), root, exe, flags, io.Discard, io.Discard); err != nil {
			t.Fatalf("%s: %v\n%s", tc.name, err, printed)
		}
		if got, want := hasDWARF(t, exe), tc.dev || tc.debugInfo; got != want {
			t.Errorf("%s: DWARF %t, want %t", tc.name, got, want)
		}
		if tc.dev {
			continue
		}
		out, err := exec.Command(exe).Output()
		if err != nil || string(out) != "v1" {
			t.Errorf("%s: printed %q, %v, want the version of GOFLAGS", tc.name, out, err)
		}
		var stderr bytes.Buffer
		run := exec.Command(exe, "panic")
		run.Stderr = &stderr
		if err := run.Run(); err == nil || !strings.Contains(stderr.String(), filepath.Join(".tifa", "main.go")+":9 ") {
			t.Errorf("%s: panic without the place of panic(\"boom\"), %v:\n%s", tc.name, err, stderr.String())
		}
	}
}

// hasDWARF says whether the executable at path has a DWARF section of debug info, read as Mach-O
// on macOS and as ELF elsewhere. The Go linker compresses them and names the one of Mach-O
// __zdebug_info. ELF keeps the name .debug_info and marks the section as compressed.
func hasDWARF(t *testing.T, path string) bool {
	t.Helper()
	var names []string
	if runtime.GOOS == "darwin" {
		f, err := macho.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, s := range f.Sections {
			names = append(names, s.Name)
		}
	} else {
		f, err := elf.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, s := range f.Sections {
			names = append(names, s.Name)
		}
	}
	for _, name := range names {
		if strings.HasSuffix(name, "debug_info") {
			return true
		}
	}
	return false
}
