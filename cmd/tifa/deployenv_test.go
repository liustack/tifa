package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvEditing(t *testing.T) {
	file := "# keys\nA=1\nexport B=old # note\n\nC=\"x y\"\nB=dup\n"
	got := withEnv(file, "B", "new value")
	want := "# keys\nA=1\nB=\"new value\"\n\nC=\"x y\"\n"
	if got != want {
		t.Errorf("replace:\n%q\nwant\n%q", got, want)
	}
	if got := withEnv("A=1\n", "D", "#fff"); got != "A=1\nD=\"#fff\"\n" {
		t.Errorf("append: %q", got)
	}
	if got := withEnv("", "D", "x"); got != "D=x\n" {
		t.Errorf("empty file: %q", got)
	}
	for in, line := range map[string]string{
		"plain":         "N=plain",
		"https://a/b:c": "N=https://a/b:c",
		"":              "N=",
		"a b":           `N="a b"`,
		`say "hi"\`:     `N="say \"hi\"\\"`,
		"two\nlines":    `N="two\nlines"`,
		"$HOME":         `N="$HOME"`,
		"=lead":         `N="=lead"`,
	} {
		if got := envLine("N", in); got != line {
			t.Errorf("%q: %s, want %s", in, got, line)
		}
	}
	if next, found := withoutEnv(file, "B"); !found || next != "# keys\nA=1\n\nC=\"x y\"\n" {
		t.Errorf("unset: %q %v", next, found)
	}
	if _, found := withoutEnv(file, "Z"); found {
		t.Error("found Z")
	}
	if got := strings.Join(envNames(file), ","); got != "A,B,C" {
		t.Errorf("names %s", got)
	}
}

// envApp makes a deployed app on the fake server: one release that passes its check.
func envApp(t *testing.T, health string) (root, home string, calls func() string) {
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	t.Setenv(deployHostEnv, "vps")
	home = t.TempDir()
	writeFile(t, filepath.Join(home, "shop", "port"), "9531\n")
	writeFile(t, filepath.Join(home, "shop", "releases", "r1", "shop"), "x")
	if err := os.Symlink("releases/r1", filepath.Join(home, "shop", "current")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, "shop", "shared", ".env"), "# mine\nA=1\n")
	calls = fakeServer(t, home, map[string]string{"r1": health})
	return root, home, calls
}

func runEnv(t *testing.T, stdin string, terminal bool, args ...string) (string, error) {
	t.Helper()
	old := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = old })
	stdinIsTerminal = func(io.Reader) bool { return terminal }
	cmd := newRootCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// set reads the value from the standard input, keeps the rest of the file and mode 600, restarts the
// app and checks it, and prints no value. list prints names. unset removes a name.
func TestEnvSetUnsetList(t *testing.T) {
	root, home, calls := envApp(t, "200")
	env := filepath.Join(home, "shop", "shared", ".env")

	out, err := runEnv(t, "s3cr3t value\n", false, "deploy", "env", "set", "API_KEY", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	data, _ := os.ReadFile(env)
	if string(data) != "# mine\nA=1\nAPI_KEY=\"s3cr3t value\"\n" {
		t.Errorf(".env:\n%s", data)
	}
	if st, _ := os.Stat(env); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	if strings.Contains(out, "s3cr3t") || strings.Contains(calls(), "s3cr3t") {
		t.Errorf("the value leaked:\n%s\n%s", out, calls())
	}
	if !strings.Contains(calls(), "systemctl --user restart tifa-shop.service") || !strings.Contains(out, "passed its health check") {
		t.Errorf("no restart:\n%s\n%s", calls(), out)
	}
	if entries, _ := filepath.Glob(filepath.Join(home, "shop", "shared", ".env.*")); len(entries) != 0 {
		t.Errorf("temporary files %v", entries)
	}

	out, err = runEnv(t, "", false, "deploy", "env", "list", "--dir", root)
	if err != nil || out != "A\nAPI_KEY\n" {
		t.Errorf("list: %v %q", err, out)
	}

	before := strings.Count(calls(), "restart")
	out, err = runEnv(t, "s3cr3t value\n", false, "deploy", "env", "set", "API_KEY", "--dir", root)
	if err != nil || !strings.Contains(out, "nothing to change") || strings.Count(calls(), "restart") != before {
		t.Errorf("same value: %v\n%s", err, out)
	}

	if out, err := runEnv(t, "", false, "deploy", "env", "unset", "API_KEY", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if data, _ := os.ReadFile(env); string(data) != "# mine\nA=1\n" {
		t.Errorf("after unset:\n%s", data)
	}
	if out, err := runEnv(t, "", false, "deploy", "env", "unset", "API_KEY", "--dir", root); err != nil || !strings.Contains(out, "is not set") {
		t.Errorf("unset again: %v %s", err, out)
	}
}

// The value never comes as an argument, and the name must be one .env can hold. On a terminal the
// value is asked for with a hidden prompt.
func TestEnvSetInput(t *testing.T) {
	root, home, _ := envApp(t, "200")
	for _, args := range [][]string{{"API_KEY=abc"}, {"1BAD"}, {"has space"}} {
		out, err := runEnv(t, "v\n", false, append([]string{"deploy", "env", "set"}, append(args, "--dir", root)...)...)
		if err == nil {
			t.Errorf("%v: no error\n%s", args, out)
		}
		if strings.Contains(out+errString(err), "abc") {
			t.Errorf("%v: the value is in the error", args)
		}
	}
	old := promptSecret
	t.Cleanup(func() { promptSecret = old })
	promptSecret = func(io.Reader, io.Writer, string) (string, error) { return "typed", nil }
	if out, err := runEnv(t, "", true, "deploy", "env", "set", "TOKEN", "--dir", root); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "shop", "shared", ".env")); !strings.Contains(string(data), "TOKEN=typed") {
		t.Errorf(".env:\n%s", data)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// A value that stops the app passing its check is rolled back: the old file comes back, the app
// restarts again, and the command says so and fails.
func TestEnvRollsBack(t *testing.T) {
	root, home, calls := envApp(t, "503")
	out, err := runEnv(t, "bad\n", false, "deploy", "env", "set", "API_KEY", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "The old .env is back") {
		t.Fatalf("got %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "shop", "shared", ".env")); string(data) != "# mine\nA=1\n" {
		t.Errorf(".env after the rollback:\n%s", data)
	}
	if n := strings.Count(calls(), "systemctl --user restart tifa-shop.service"); n != 2 {
		t.Errorf("%d restarts:\n%s", n, calls())
	}
	if !strings.Contains(out, "health check: GET /healthz answered 503") {
		t.Errorf("out:\n%s", out)
	}
}

// An app that is not on the server yet has no .env to edit.
func TestEnvNeedsTheApp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	t.Setenv(deployHostEnv, "vps")
	fakeServer(t, t.TempDir(), nil)
	if _, err := runEnv(t, "", false, "deploy", "env", "list", "--dir", root); err == nil || !strings.Contains(err.Error(), "tifa deploy first") {
		t.Errorf("got %v", err)
	}
}
