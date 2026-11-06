package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"
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

// forwardHelperEnv makes the test binary run forward with the folder in the variable and the
// arguments after "--", as main does, since forward does not return when it works.
const forwardHelperEnv = "TIFA_TEST_FORWARD_ROOT"

func TestMain(m *testing.M) {
	if root := os.Getenv(forwardHelperEnv); root != "" {
		args := os.Args[slices.Index(os.Args, "--")+1:]
		if err := forward(root, args); err != nil {
			fmt.Fprintln(os.Stderr, "tifa:", err)
		}
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// forwardHelper starts this test binary forwarding args to the app at root, with a fake go first
// on PATH that runs script.
func forwardHelper(t *testing.T, root, script string, args ...string) *exec.Cmd {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), forwardHelperEnv+"="+root, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return cmd
}

// The forwarded command gets the arguments, the folder of the app and the marker, and its output
// and exit status are those of tifa.
func TestForwardRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake go is a shell script")
	}
	app := writeMod(t, "v0.3.0", toolLine)
	log := filepath.Join(t.TempDir(), "log")
	cmd := forwardHelper(t, app, "echo \"$PWD|$*|$"+forwardedEnv+"\" > '"+log+"'\necho out\necho err >&2\nexit 7\n", "build", "-o", "x y")
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit = %v, want status 7\nstderr: %s", err, errOut.String())
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

// A SIGINT sent to tifa alone, not to its process group, as a test or a supervisor sends it,
// reaches the forwarded command.
func TestForwardPassesSIGINT(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no SIGINT on Windows")
	}
	app := writeMod(t, "v0.3.0", toolLine)
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := forwardHelper(t, app, "trap 'echo got-int; exit 3' INT\ntouch '"+ready+"'\nwhile :; do sleep 0.05; done\n", "dev")
	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if i == 200 {
			cmd.Process.Kill()
			t.Fatal("the fake go did not start")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 3 || out.String() != "got-int\n" {
			t.Fatalf("exit = %v, output %q, want the forwarded command to stop on the SIGINT with 3", err, out.String())
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("tifa did not stop after SIGINT")
	}
}

func TestForwardCannotStart(t *testing.T) {
	app := writeMod(t, "v0.3.0", toolLine)
	t.Setenv("PATH", t.TempDir())
	err := forward(app, []string{"dev"})
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
