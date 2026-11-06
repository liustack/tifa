package main

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

const toolLine = "tool github.com/liustack/tifa/cmd/tifa\n"

// writeMod writes a go.mod that requires tifa at version into a new folder.
func writeMod(t *testing.T, version, extra string) string {
	t.Helper()
	dir := t.TempDir()
	mod := "module example.com/app\n\ngo 1.27.0\n\nrequire github.com/liustack/tifa " + version + "\n\n" + extra
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func releaseInfo(version string) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: version, Sum: "h1:x"}}
}

func noEnv(string) string { return "" }

func TestForwardTarget(t *testing.T) {
	app := writeMod(t, "v0.3.0", toolLine)
	replaced := writeMod(t, "v0.3.0", toolLine+"replace github.com/liustack/tifa => ../tifa\n")
	noTool := writeMod(t, "v0.3.0", "")
	plain := t.TempDir()
	sub := filepath.Join(app, "app", "todos")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	checkout := &debug.BuildInfo{Main: debug.Module{Path: tifaModule, Version: "(devel)"}}
	cases := []struct {
		name string
		args []string
		cwd  string
		info *debug.BuildInfo
		env  func(string) string
		want string
	}{
		{"same version runs itself", []string{"dev"}, app, releaseInfo("v0.3.0"), noEnv, ""},
		{"another version forwards", []string{"dev"}, app, releaseInfo("v0.2.0"), noEnv, app},
		{"checkout build forwards", []string{"dev"}, app, checkout, noEnv, app},
		{"no build info forwards", []string{"dev"}, app, nil, noEnv, app},
		{"replace forwards", []string{"dev"}, replaced, releaseInfo("v0.3.0"), noEnv, replaced},
		{"no tool line runs itself", []string{"dev"}, noTool, releaseInfo("v0.2.0"), noEnv, ""},
		{"outside a module runs itself", []string{"dev"}, plain, releaseInfo("v0.2.0"), noEnv, ""},
		{"subfolder finds the app", []string{"dev"}, sub, releaseInfo("v0.2.0"), noEnv, app},
		{"dir flag picks the app", []string{"build", "--dir", app}, plain, releaseInfo("v0.2.0"), noEnv, app},
		{"dir=flag picks the app", []string{"build", "--dir=" + app}, plain, releaseInfo("v0.2.0"), noEnv, app},
		{"relative dir flag", []string{"build", "--dir", filepath.Base(app)}, filepath.Dir(app), releaseInfo("v0.2.0"), noEnv, app},
		{"marker stops a second forward", []string{"dev"}, app, releaseInfo("v0.2.0"), func(k string) string {
			if k == forwardedEnv {
				return "1"
			}
			return ""
		}, ""},
		{"new never forwards", []string{"new", "x"}, app, releaseInfo("v0.2.0"), noEnv, ""},
		{"go.work forces forwarding", []string{"dev"}, app, releaseInfo("v0.3.0"), func(k string) string {
			if k == "GOWORK" {
				return filepath.Join(app, "go.work")
			}
			return ""
		}, app},
		{"GOWORK=off keeps the rule", []string{"dev"}, app, releaseInfo("v0.3.0"), func(k string) string {
			if k == "GOWORK" {
				return "off"
			}
			return ""
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := forwardTarget(tc.args, tc.cwd, tc.info, tc.env)
			if ok != (tc.want != "") || got != tc.want {
				t.Fatalf("forwardTarget = %q, %v, want %q", got, ok, tc.want)
			}
		})
	}
}

func TestForwardTargetGoWorkFile(t *testing.T) {
	app := writeMod(t, "v0.3.0", toolLine)
	if err := os.WriteFile(filepath.Join(app, "go.work"), []byte("go 1.27.0\n\nuse .\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := forwardTarget([]string{"dev"}, app, releaseInfo("v0.3.0"), noEnv); !ok || got != app {
		t.Fatalf("forwardTarget = %q, %v, want to forward to %q", got, ok, app)
	}
}

// TestForwardRuns puts a fake go on PATH that records its arguments, folder and environment.
func TestForwardRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake go is a shell script")
	}
	app := writeMod(t, "v0.3.0", toolLine)
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "log")
	script := "#!/bin/sh\necho \"$PWD|$*|$" + forwardedEnv + "\" > '" + log + "'\necho out\necho err >&2\nexit 7\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out, errOut strings.Builder
	code, err := forward(app, []string{"build", "-o", "x y"}, nil, &out, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Fatalf("exit status = %d, want 7", code)
	}
	if out.String() != "out\n" || errOut.String() != "err\n" {
		t.Fatalf("output = %q, %q", out.String(), errOut.String())
	}
	b, _ := os.ReadFile(log)
	want := "|tool tifa build -o x y|1\n"
	got := string(b)
	if !strings.HasSuffix(got, want) || !strings.Contains(got, filepath.Base(app)) {
		t.Fatalf("fake go saw %q, want folder of the app and %q", got, want)
	}
}

func TestForwardCannotStart(t *testing.T) {
	app := writeMod(t, "v0.3.0", toolLine)
	t.Setenv("PATH", t.TempDir())
	_, err := forward(app, []string{"dev"}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "go tool tifa dev") {
		t.Fatalf("err = %v, want it to name the command to run", err)
	}
}

// A tifa that the go command runs, go tool tifa or go run, lives in the build cache and is the
// version the app pins already, so it never forwards.
func TestRunByGoCommand(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "go-build")
	env := func(k string) string {
		if k == "GOCACHE" {
			return cache
		}
		return ""
	}
	for _, tc := range []struct {
		exe  string
		want bool
	}{
		{filepath.Join(cache, "d0", "d063eb-d", "tifa"), true},
		{filepath.Join(filepath.Dir(cache), "bin", "tifa"), false},
		{cache + "-other" + string(filepath.Separator) + "tifa", false},
	} {
		if got := runByGoCommand(tc.exe, env); got != tc.want {
			t.Errorf("runByGoCommand(%s) = %v, want %v", tc.exe, got, tc.want)
		}
	}
}
