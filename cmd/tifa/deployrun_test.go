package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// runApp is an app with one release on the fake server whose program prints its arguments, the
// folder it runs in, the variables of HTTPS and the .env there, writes to stderr, and exits with
// the status its arguments ask for: exit <status>.
func runApp(t *testing.T) (root, home string, calls func() string) {
	t.Helper()
	root, home, calls = envApp(t, "200")
	program := filepath.Join(home, "shop", "releases", "r1", "shop")
	writeFile(t, program, `#!/bin/sh
if [ "$1" = exit ]; then echo "exiting with $2" >&2; exit "$2"; fi
for a in "$@"; do printf 'arg <%s>\n' "$a"; done
echo "folder $(basename "$PWD")"
echo "https <${SERVER_TLS_DOMAINS-}> <${SERVER_TLS_CACHE-}> <${SERVER_TLS_CERT-}> <${SERVER_TLS_KEY-}>"
cat .env
echo "to stderr" >&2
`)
	if err := os.Chmod(program, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, home, calls
}

// runRun runs tifa with args as from a terminal or not, and returns its output, its error output
// and its error.
func runRun(t *testing.T, terminal bool, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	oldIn, oldOut := stdinIsTerminal, stdoutIsTerminal
	t.Cleanup(func() { stdinIsTerminal, stdoutIsTerminal = oldIn, oldOut })
	stdinIsTerminal = func(io.Reader) bool { return terminal }
	stdoutIsTerminal = func(io.Writer) bool { return terminal }
	return run(t, args...)
}

// deploy run runs the program of the current release as tifa, in shared/ where its .env is, with
// the arguments as they were given, however the shell would read them, and passes its output, its
// error output and its exit status through.
func TestDeployRun(t *testing.T) {
	root, _, calls := runApp(t)
	args := []string{"seed", "--count", "3", "two words", "it's", `"quoted"`, "$HOME", "`id`", "a;b", "", "line\nbreak", "--dir", "elsewhere"}
	out, errOut, err := runRun(t, false, append([]string{"deploy", "run", "--dir", root}, args...)...)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	var want strings.Builder
	for _, a := range args {
		want.WriteString("arg <" + a + ">\n")
	}
	want.WriteString("folder shared\nhttps <> <> <> <>\n# mine\nA=1\n")
	if out != want.String() {
		t.Errorf("output:\n%s\nwant:\n%s", out, want.String())
	}
	if !strings.Contains(errOut, "to stderr\n") {
		t.Errorf("error output %q", errOut)
	}
	if strings.Contains(calls(), "ssh root") || strings.Count(calls(), "ssh tifa vps") != 2 {
		t.Errorf("calls, want a look and the run, both as tifa:\n%s", calls())
	}
	// -- ends the flags of tifa too.
	out, _, err = runRun(t, false, "deploy", "run", "--dir", root, "--", "--help")
	if err != nil || out != "arg <--help>\nfolder shared\nhttps <> <> <> <>\n# mine\nA=1\n" {
		t.Errorf("-- --help: %v\n%s", err, out)
	}
}

// The exit status of the program is that of deploy run, and deploy run adds no message of its own.
func TestDeployRunExitStatus(t *testing.T) {
	root, _, _ := runApp(t)
	_, errOut, err := runRun(t, false, "deploy", "run", "--dir", root, "exit", "3")
	var exit *exitError
	if !errors.As(err, &exit) || exit.code != 3 || exit.err != nil {
		t.Fatalf("exit 3: %#v", err)
	}
	if errOut != "exiting with 3\n" {
		t.Errorf("error output %q", errOut)
	}
}

// An app that serves HTTPS itself has the variables of its service.
func TestDeployRunHTTPS(t *testing.T) {
	root, home, _ := runApp(t)
	writeFile(t, filepath.Join(home, "shop", "https"), "shop.example.com\n")
	out, _, err := runRun(t, false, "deploy", "run", "--dir", root, "x")
	want := "https <shop.example.com> <" + filepath.Join(home, "shop", "certs") + "> <> <>\n"
	if err != nil || !strings.Contains(out, want) {
		t.Errorf("%v\n%s\nwant %q", err, out, want)
	}
}

// With a terminal on both ends, ssh gets one too, for a command that asks something. Without, it
// runs in BatchMode and gets none, so a pipe keeps the bytes it carries.
func TestDeployRunTerminal(t *testing.T) {
	root, _, _ := runApp(t)
	for _, terminal := range []bool{true, false} {
		if _, _, err := runRun(t, terminal, "deploy", "run", "--dir", root, "x"); err != nil {
			t.Fatal(err)
		}
		// Each call starts a line with -l, and its script goes on over more lines.
		var last []string
		for _, line := range strings.Split(fakeServerArgv(), "\n") {
			if strings.HasPrefix(line, "-l ") {
				last = strings.Fields(line)
			}
		}
		before := last[:slices.Index(last, "--")]
		if slices.Contains(before, "-t") != terminal || slices.Contains(before, "BatchMode=yes") == terminal {
			t.Errorf("terminal %v: ssh got %q", terminal, before)
		}
	}
}

// A run needs a command and a release, and the server that ~/.tifa remembers.
func TestDeployRunRefuses(t *testing.T) {
	root, home, calls := runApp(t)
	if _, _, err := runRun(t, false, "deploy", "run", "--dir", root); err == nil || !strings.Contains(err.Error(), "requires at least 1 arg") {
		t.Errorf("no command: %v", err)
	}
	if err := os.Remove(filepath.Join(home, "shop", "current")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runRun(t, false, "deploy", "run", "--dir", root, "seed"); err == nil || !strings.Contains(err.Error(), "shop has no release on vps yet: run go tool tifa deploy first") {
		t.Errorf("no release: %v", err)
	}
	if strings.Count(calls(), "ssh tifa vps") != 1 {
		t.Errorf("ran without a release:\n%s", calls())
	}

	root, _, calls = runApp(t)
	homeOf(t)
	t.Setenv(deployHostEnv, "")
	if _, err := rememberServer("another-1a2b3c4d", "vps", knownApp{Name: "shop", Path: root}); err != nil {
		t.Fatal(err)
	}
	out, _, err := runRun(t, false, "deploy", "run", "--dir", root, "seed")
	if err == nil || !strings.Contains(err.Error(), "is not the server") || strings.Contains(out, "arg <seed>") {
		t.Errorf("another machine: %v\n%s", err, out)
	}
}
