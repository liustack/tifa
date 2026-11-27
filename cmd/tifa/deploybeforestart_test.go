package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// beforeStartApp is an app on the fake server with deploy.before_start set to command, whose
// program writes to $HOME/ran a line for each run: its release, the release current points at, the
// folder it runs in, the .env it reads and its arguments. It exits with 3 while $HOME/fail exists.
// Every release passes its health check.
func beforeStartApp(t *testing.T, command string) (root, home string, calls func() string, ran func() string) {
	t.Helper()
	ids := []string{"20261120T100000Z", "20261120T110000Z", "20261120T120000Z", "20261120T130000Z"}
	health := map[string]string{}
	for _, id := range ids {
		health[id] = "200"
	}
	root, home, calls = serveApp(t, "9531", "", health)
	if command != "" {
		writeFile(t, filepath.Join(root, "tifa.yaml"), "deploy:\n  before_start: "+command+"\n")
	}
	writeFile(t, filepath.Join(home, "shop", "shared", ".env"), "A=1\n")
	buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
		return os.WriteFile(out, []byte(`#!/bin/sh
self=$(basename "$(dirname "$0")")
cur=$(readlink "$HOME/shop/current" || echo none)
words=""
for a in "$@"; do words="$words <$a>"; done
echo "$self current=${cur#releases/} folder=$(basename "$PWD") env=$(cat .env)$words" >> "$HOME/ran"
echo "migrating with $self"
if [ -f "$HOME/fail" ]; then echo "migrations: no such table: nope" >&2; exit 3; fi
`), 0o755)
	}
	ran = func() string {
		data, _ := os.ReadFile(filepath.Join(home, "ran"))
		return string(data)
	}
	return root, home, calls, ran
}

// deploy.before_start runs the command with the program of the new release, after the upload and
// before current points at it, as tifa in shared/ with its .env, as deploy run does. Its output
// is part of the output of the deploy.
func TestDeployBeforeStartRunsTheNewRelease(t *testing.T) {
	root, home, calls, ran := beforeStartApp(t, "migrate --to 3")
	out, _, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("first deploy: %v\n%s", err, out)
	}
	out, _, err = run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("second deploy: %v\n%s", err, out)
	}
	want := "20261120T100000Z current=none folder=shared env=A=1 <migrate> <--to> <3>\n" +
		"20261120T110000Z current=20261120T100000Z folder=shared env=A=1 <migrate> <--to> <3>\n"
	if ran() != want {
		t.Errorf("ran:\n%s\nwant:\n%s", ran(), want)
	}
	if !strings.Contains(out, "deploy.before_start: migrate --to 3, with release 20261120T110000Z\nmigrating with 20261120T110000Z\n") {
		t.Errorf("the output does not show the command and its output:\n%s", out)
	}
	// The command runs before the restart: the run of the second deploy comes before its restart.
	c := calls()
	second := c[strings.LastIndex(c, "ssh tifa vps"):]
	if strings.Count(c, "ssh tifa vps") != 2*4 || !strings.Contains(second, "systemctl --user restart tifa-shop.service") {
		t.Errorf("calls, want a look, the upload, the command and the switch for each deploy:\n%s", c)
	}
	if cur, _ := os.Readlink(filepath.Join(home, "shop", "current")); cur != "releases/20261120T110000Z" {
		t.Errorf("current %s", cur)
	}
}

// A command that fails stops the deploy: current stays, nothing restarts, the release is recorded as
// failed and is no place a rollback can go.
func TestDeployBeforeStartFailureStopsTheDeploy(t *testing.T) {
	root, home, calls, ran := beforeStartApp(t, "migrate")
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("first deploy: %v\n%s", err, out)
	}
	writeFile(t, filepath.Join(home, "fail"), "")
	restarts := strings.Count(calls(), "systemctl --user restart")
	out, stderr, err := run(t, "deploy", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "release 20261120T110000Z failed deploy.before_start, so it was not started: 20261120T100000Z still runs") {
		t.Fatalf("failing command: %v\n%s%s", err, out, stderr)
	}
	if !strings.Contains(stderr, "migrations: no such table: nope\ntifa: migrate exited with status 3\n") || !strings.Contains(out, "migrating with 20261120T110000Z") {
		t.Errorf("the output of the command is not shown:\n%s\n%s", out, stderr)
	}
	if cur, _ := os.Readlink(filepath.Join(home, "shop", "current")); cur != "releases/20261120T100000Z" {
		t.Errorf("current %s", cur)
	}
	if strings.Count(calls(), "systemctl --user restart") != restarts {
		t.Errorf("the failed deploy restarted the service:\n%s", calls())
	}
	log, _ := os.ReadFile(filepath.Join(home, "shop", "deploys.log"))
	if lines := strings.Split(strings.TrimSpace(string(log)), "\n"); len(lines) != 2 || !strings.HasSuffix(lines[1], "\t20261120T110000Z\tnone\tdeploy\tfailed-before-start") {
		t.Errorf("deploys.log:\n%s", log)
	}
	out, _, err = run(t, "deploy", "rollback", "20261120T110000Z", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "release 20261120T110000Z failed deploy.before_start when it was deployed") {
		t.Errorf("rollback to the failed release: %v\n%s", err, out)
	}
	out, _, err = run(t, "deploy", "status", "--dir", root)
	if err != nil || !strings.Contains(out, "20261120T110000Z  commit none  failed deploy.before_start") {
		t.Errorf("status: %v\n%s", err, out)
	}
	// Not one more run of the command by the rollback or the status.
	if n := strings.Count(ran(), "\n"); n != 2 {
		t.Errorf("ran %d times:\n%s", n, ran())
	}
}

// The first deploy has no release to keep: a failed command leaves no current and starts nothing.
func TestDeployBeforeStartFailsTheFirstDeploy(t *testing.T) {
	root, home, calls, _ := beforeStartApp(t, "migrate")
	writeFile(t, filepath.Join(home, "fail"), "")
	_, _, err := run(t, "deploy", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "release 20261120T100000Z failed deploy.before_start, so it was not started: no release runs yet") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, "shop", "current")); err == nil {
		t.Error("current was made")
	}
	if strings.Contains(calls(), "systemctl --user") {
		t.Errorf("the service was touched:\n%s", calls())
	}
}

// Each word of the setting reaches the program as it is written, whatever a shell would make of it.
func TestDeployBeforeStartQuotesTheWords(t *testing.T) {
	root, home, _, ran := beforeStartApp(t, "migrate $HOME `touch${IFS}pwned` a;b && * > out |x")
	if out, _, err := run(t, "deploy", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if want := "<migrate> <$HOME> <`touch${IFS}pwned`> <a;b> <&&> <*> <>> <out> <|x>\n"; !strings.HasSuffix(ran(), want) {
		t.Errorf("ran %q, want the words %q", ran(), want)
	}
	for _, name := range []string{"pwned", "out"} {
		for _, dir := range []string{home, filepath.Join(home, "shop", "shared")} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				t.Errorf("a shell ran a word: %s is there", filepath.Join(dir, name))
			}
		}
	}
}

// A rollback, to the release before or to one named, starts a release that ran its command when it
// was deployed, so it runs no command.
func TestDeployRollbackSkipsBeforeStart(t *testing.T) {
	root, _, _, ran := beforeStartApp(t, "migrate")
	for range 3 {
		if out, _, err := run(t, "deploy", "--dir", root); err != nil {
			t.Fatalf("deploy: %v\n%s", err, out)
		}
	}
	before := ran()
	if out, _, err := run(t, "deploy", "rollback", "--dir", root); err != nil {
		t.Fatalf("rollback: %v\n%s", err, out)
	}
	if out, _, err := run(t, "deploy", "rollback", "20261120T120000Z", "--dir", root); err != nil {
		t.Fatalf("rollback to a release: %v\n%s", err, out)
	}
	if ran() != before {
		t.Errorf("a rollback ran the command:\n%s", strings.TrimPrefix(ran(), before))
	}
}

// Without the setting a deploy is a look, the upload and the switch, and no program runs before it.
func TestDeployWithoutBeforeStart(t *testing.T) {
	root, _, calls, ran := beforeStartApp(t, "")
	out, _, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if ran() != "" || strings.Count(calls(), "ssh tifa vps") != 3 || strings.Contains(out, "before_start") {
		t.Errorf("ran %q\n%s\n%s", ran(), calls(), out)
	}
}
