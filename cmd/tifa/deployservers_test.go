package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// homeOf points ~/.tifa at a temporary folder for the test and returns it. The real one is never
// read or written.
func homeOf(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tifa-home")
	t.Setenv(tifaHomeEnv, dir)
	return dir
}

func readFileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The folder is TIFA_HOME, else .tifa in the home folder of the user (USERPROFILE on Windows).
func TestTifaHome(t *testing.T) {
	t.Setenv(tifaHomeEnv, "/somewhere/tifa")
	if got, err := tifaHome(); err != nil || got != filepath.FromSlash("/somewhere/tifa") {
		t.Errorf("TIFA_HOME: %q, %v", got, err)
	}
	t.Setenv(tifaHomeEnv, "")
	user := t.TempDir()
	t.Setenv("HOME", user)
	t.Setenv("USERPROFILE", user)
	if got, err := tifaHome(); err != nil || got != filepath.Join(user, ".tifa") {
		t.Errorf("home: %q, %v", got, err)
	}
}

// Setup writes the file with a header, one entry per server, and the apps it put there.
func TestServersRemember(t *testing.T) {
	dir := homeOf(t)
	if got, err := readServers(); err != nil || len(got) != 0 {
		t.Fatalf("no file yet: %v, %v", got, err)
	}
	if _, err := rememberServer("vps-1a2b3c4d", "ssh://203.0.113.7:22", "shop"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	text := readFileText(t, path)
	for _, want := range []string{"# Servers tifa deploy setup has set up from this machine", "servers:", "id: vps-1a2b3c4d", "host: ssh://203.0.113.7:22", "apps: [shop]"} {
		if !strings.Contains(text, want) {
			t.Errorf("file lacks %q:\n%s", want, text)
		}
	}
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
			if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != want {
				t.Errorf("%s: %v, %v", p, fi, err)
			}
		}
	}

	// The same server again: its host is the one that worked last, and the apps add up once.
	for _, app := range []string{"blog", "shop"} {
		if _, err := rememberServer("vps-1a2b3c4d", "vps-ssh-k3j9.example.com", app); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readServers()
	if err != nil || len(got) != 1 || got[0].Host != "vps-ssh-k3j9.example.com" || strings.Join(got[0].Apps, ",") != "shop,blog" {
		t.Fatalf("one server: %+v, %v", got, err)
	}
	if got[0].name() != "vps" {
		t.Errorf("name %q", got[0].name())
	}

	// Another server. One that moved to the host of this one is the same place, so it goes.
	if _, err := rememberServer("hz-99998888", "ssh://198.51.100.4:22", "notes"); err != nil {
		t.Fatal(err)
	}
	if _, err := rememberServer("new-11112222", "ssh://198.51.100.4:22", "notes"); err != nil {
		t.Fatal(err)
	}
	got, _ = readServers()
	if len(got) != 2 || got[1].ID != "new-11112222" {
		t.Errorf("a server that took over a host: %+v", got)
	}
}

// What the person wrote in the file stays, and other keys too.
func TestServersKeepComments(t *testing.T) {
	dir := homeOf(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config.yaml"), "# mine\nother: 1\nservers:\n  # the small one\n  - id: vps-1\n    host: vps\n    apps: [shop]\n")
	if _, err := rememberServer("vps-1", "vps", "blog"); err != nil {
		t.Fatal(err)
	}
	text := readFileText(t, filepath.Join(dir, "config.yaml"))
	for _, want := range []string{"# mine", "other: 1", "# the small one", "apps: [shop, blog]"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
}

// remove drops the app, and the entry with the last app.
func TestServersForget(t *testing.T) {
	dir := homeOf(t)
	for _, app := range []string{"shop", "blog"} {
		if _, err := rememberServer("vps-1", "vps", app); err != nil {
			t.Fatal(err)
		}
	}
	if err := forgetServerApp("vps-1", "shop"); err != nil {
		t.Fatal(err)
	}
	if got, _ := readServers(); len(got) != 1 || strings.Join(got[0].Apps, ",") != "blog" {
		t.Errorf("after one: %+v", got)
	}
	if err := forgetServerApp("vps-1", "blog"); err != nil {
		t.Fatal(err)
	}
	if got, _ := readServers(); len(got) != 0 {
		t.Errorf("after the last: %+v", got)
	}
	if text := readFileText(t, filepath.Join(dir, "config.yaml")); !strings.Contains(text, "servers: []") || !strings.Contains(text, "# Servers") {
		t.Errorf("file:\n%s", text)
	}
	// Nothing to forget is not an error, and a missing file is not made.
	if err := forgetServerApp("gone", "x"); err != nil {
		t.Error(err)
	}
	other := homeOf(t)
	if err := forgetServerApp("vps-1", "shop"); err != nil || exists(filepath.Join(other, "config.yaml")) {
		t.Errorf("forget without a file: %v", err)
	}
}

// A file that is not what tifa wrote is an error that names it, and is left alone.
func TestServersBadFile(t *testing.T) {
	dir := homeOf(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	for _, bad := range []string{"servers: [", "servers: 3\n", "servers:\n  - host: x\n", "- a\n"} {
		writeFile(t, path, bad)
		if _, err := readServers(); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%q read: %v", bad, err)
		}
		if _, err := rememberServer("vps-1", "vps", "shop"); err == nil {
			t.Errorf("%q written over", bad)
		}
		if readFileText(t, path) != bad {
			t.Errorf("%q changed", bad)
		}
	}
}

// Several tifa processes setting up at the same time lose no entry.
func TestServersConcurrent(t *testing.T) {
	homeOf(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := rememberServer(fmt.Sprintf("vps-%d", i), fmt.Sprintf("10.0.0.%d", i), "app"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got, err := readServers(); err != nil || len(got) != 12 {
		t.Errorf("%d servers: %v", len(got), err)
	}
}

// The server of an app: the only one, the one that lists the app, else a list to choose from.
func TestPickServer(t *testing.T) {
	one := []knownServer{{ID: "vps-1", Host: "ssh://203.0.113.7:22", Apps: []string{"blog"}}}
	two := append(one, knownServer{ID: "hz-2", Host: "ssh://198.51.100.4:22", Apps: []string{"shop"}})
	if s, ok := pickServer(nil, "shop"); ok || s.ID != "" {
		t.Errorf("none: %+v", s)
	}
	if s, ok := pickServer(one, "shop"); !ok || s.ID != "vps-1" {
		t.Errorf("the only one: %+v", s)
	}
	if s, ok := pickServer(two, "shop"); !ok || s.ID != "hz-2" {
		t.Errorf("the one with the app: %+v", s)
	}
	if _, ok := pickServer(two, "notes"); ok {
		t.Error("two servers, and no app in either")
	}
	both := []knownServer{two[0], {ID: "hz-2", Host: "h", Apps: []string{"blog"}}}
	if _, ok := pickServer(both, "blog"); ok {
		t.Error("an app on two servers is not a choice setup made")
	}
}

// loadTarget is loadDeployTarget with its one-line notice thrown away.
func loadTarget(dir, hostFlag string) (deployTarget, error) {
	return loadDeployTarget(dir, hostFlag, io.Discard)
}

// shopApp is a bare app called shop.
func shopApp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	t.Setenv(deployHostEnv, "")
	return root
}

// The server comes from the flag, the environment and .env before ~/.tifa. With no server named,
// the only server of ~/.tifa is used, and a line says so and what to write to choose.
func TestDeployHostFromHome(t *testing.T) {
	homeOf(t)
	root := shopApp(t)
	if _, err := rememberServer("vps-1a2b3c4d", "ssh://203.0.113.7:22", "blog"); err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	d, err := loadDeployTarget(root, "", &said)
	if err != nil || d.host != "ssh://203.0.113.7:22" || d.dest.port != 22 {
		t.Fatalf("%+v, %v", d, err)
	}
	line := strings.TrimSpace(said.String())
	for _, want := range []string{"No server is named for shop", "vps", "ssh://203.0.113.7:22", "config.yaml", "TIFA_DEPLOY_HOST=ssh://203.0.113.7:22", ".env", "--host"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q: %s", want, line)
		}
	}
	if strings.Contains(line, "\n") {
		t.Errorf("more than one line: %q", line)
	}

	// Anything named wins, and then nothing is said.
	said.Reset()
	if d, err := loadDeployTarget(root, "from-flag", &said); err != nil || d.host != "from-flag" || said.Len() != 0 {
		t.Errorf("flag: %+v, %v, %q", d, err, said.String())
	}
	t.Setenv(deployHostEnv, "from-env")
	if d, err := loadDeployTarget(root, "", &said); err != nil || d.host != "from-env" || said.Len() != 0 {
		t.Errorf("env: %+v, %v, %q", d, err, said.String())
	}
	t.Setenv(deployHostEnv, "")
	writeFile(t, filepath.Join(root, ".env"), "TIFA_DEPLOY_HOST=from-dotenv\n")
	if d, err := loadDeployTarget(root, "", &said); err != nil || d.host != "from-dotenv" || said.Len() != 0 {
		t.Errorf(".env: %+v, %v, %q", d, err, said.String())
	}
}

// With several servers and none named, the command stops and lists them, unless the app is on one.
func TestDeployHostSeveralInHome(t *testing.T) {
	homeOf(t)
	root := shopApp(t)
	for _, s := range []struct{ id, host, app string }{
		{"vps-1a2b3c4d", "ssh://203.0.113.7:22", "blog"},
		{"hz-99998888", "vps-ssh-k3j9.example.com", "notes"},
	} {
		if _, err := rememberServer(s.id, s.host, s.app); err != nil {
			t.Fatal(err)
		}
	}
	_, err := loadTarget(root, "")
	if err == nil {
		t.Fatal("two servers, none named, and no error")
	}
	for _, want := range []string{"shop", "vps", "ssh://203.0.113.7:22", "apps: blog", "hz", "vps-ssh-k3j9.example.com", "apps: notes", "TIFA_DEPLOY_HOST=ssh://203.0.113.7:22", ".env", "--host"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%s", want, err)
		}
	}
	// An app that setup put on one of them goes there, without a question.
	if _, err := rememberServer("hz-99998888", "vps-ssh-k3j9.example.com", "shop"); err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	d, err := loadDeployTarget(root, "", &said)
	if err != nil || d.host != "vps-ssh-k3j9.example.com" || !strings.Contains(said.String(), "shop is set up on hz") {
		t.Errorf("%+v, %v, %q", d, err, said.String())
	}
}

// With none, the error says where to name the server and that setup remembers it.
func TestDeployHostNoneInHome(t *testing.T) {
	homeOf(t)
	_, err := loadTarget(shopApp(t), "")
	if err == nil {
		t.Fatal("no server")
	}
	for _, want := range []string{"no server to deploy to", "--host", "TIFA_DEPLOY_HOST=ssh://203.0.113.7:22", "deploy setup", "config.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%s", want, err)
		}
	}
}

// A file of ~/.tifa that cannot be read stops the command and names the file, when it is needed.
func TestDeployHostBadHomeFile(t *testing.T) {
	dir := homeOf(t)
	writeFile(t, filepath.Join(dir, "config.yaml"), "servers: [")
	root := shopApp(t)
	if _, err := loadTarget(root, ""); err == nil || !strings.Contains(err.Error(), "config.yaml") {
		t.Errorf("got %v", err)
	}
	if d, err := loadTarget(root, "vps"); err != nil || d.host != "vps" {
		t.Errorf("a named server does not read the file: %+v, %v", d, err)
	}
}
