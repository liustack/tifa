package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

// app is an app of the test: its checkout is /work/<name>.
func app(name string) knownApp {
	return knownApp{Name: name, Path: filepath.FromSlash("/work/" + name)}
}

// Setup writes the file with a header, one entry per server, and the apps it put there.
func TestServersRemember(t *testing.T) {
	dir := homeOf(t)
	if got, err := readServers(); err != nil || len(got) != 0 {
		t.Fatalf("no file yet: %v, %v", got, err)
	}
	if changed, err := rememberServer("vps-1a2b3c4d", "ssh://203.0.113.7:22", app("shop")); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := rememberServer("vps-1a2b3c4d", "ssh://203.0.113.7:22", app("shop")); err != nil || changed {
		t.Fatal("the same again changed the file:", changed, err)
	}
	path := filepath.Join(dir, "config.yaml")
	text := readFileText(t, path)
	for _, want := range []string{"# Servers tifa deploy setup has set up from this machine", "servers:", "id: vps-1a2b3c4d", "host: ssh://203.0.113.7:22", "name: shop", "host: 'ssh://203.0.113.7:22'}"} {
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

	// The same server again: its host is the one that worked last, each app keeps its own, and an
	// app counts once.
	for _, a := range []string{"blog", "shop"} {
		if _, err := rememberServer("vps-1a2b3c4d", "vps-"+a+".example.com", app(a)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readServers()
	if err != nil || len(got) != 1 || got[0].Host != "vps-shop.example.com" || len(got[0].Apps) != 2 || got[0].Apps[0].Host != "vps-shop.example.com" || got[0].Apps[1].Host != "vps-blog.example.com" {
		t.Fatalf("one server: %+v, %v", got, err)
	}
	if got[0].name() != "vps" || got[0].appNames() != "shop, blog" {
		t.Errorf("name %q, apps %q", got[0].name(), got[0].appNames())
	}

	// Another server. One that moved to the host of this one is the same place, so it goes.
	for _, id := range []string{"hz-99998888", "new-11112222"} {
		if _, err := rememberServer(id, "ssh://198.51.100.4:22", app("notes")); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = readServers()
	if len(got) != 2 || got[1].ID != "new-11112222" {
		t.Errorf("a server that took over a host: %+v", got)
	}
}

// An app is its checkout: the same folder is one app, and a folder with the same name is another.
// An app that is set up on another server leaves the first one.
func TestServersAppsAreFolders(t *testing.T) {
	homeOf(t)
	one := knownApp{Name: "shop", Path: filepath.FromSlash("/work/acme/shop")}
	two := knownApp{Name: "shop", Path: filepath.FromSlash("/work/other/shop")}
	for _, a := range []knownApp{one, two} {
		if _, err := rememberServer("vps-1", "vps", a); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := readServers()
	if len(got) != 1 || len(got[0].Apps) != 2 {
		t.Fatalf("two checkouts: %+v", got)
	}
	if _, err := rememberServer("hz-2", "hz", one); err != nil {
		t.Fatal(err)
	}
	got, _ = readServers()
	if len(got) != 2 || len(got[0].Apps) != 1 || got[0].Apps[0].Path != two.Path || got[1].Apps[0].Path != one.Path {
		t.Errorf("moved: %+v", got)
	}
}

// What the person wrote in the file stays, and other keys too.
func TestServersKeepComments(t *testing.T) {
	dir := homeOf(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config.yaml"), "# mine\nother: 1\nservers:\n  # the small one\n  - id: vps-1\n    host: vps\n    apps:\n      - {name: shop, path: /work/shop, host: vps}\n")
	if _, err := rememberServer("vps-1", "vps", app("blog")); err != nil {
		t.Fatal(err)
	}
	text := readFileText(t, filepath.Join(dir, "config.yaml"))
	for _, want := range []string{"# mine", "other: 1", "# the small one", "name: blog"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
}

// remove drops the app, and the entry with the last app. The host of the server moves to an app
// that stays when it was the host of the one that went.
func TestServersForget(t *testing.T) {
	dir := homeOf(t)
	for _, a := range []string{"shop", "blog", "notes"} {
		if _, err := rememberServer("vps-1", a+".example.com", app(a)); err != nil {
			t.Fatal(err)
		}
	}
	// notes was last: its host is the server's. Take it away.
	if err := forgetServerApp("vps-1", app("notes").Path); err != nil {
		t.Fatal(err)
	}
	if got, _ := readServers(); len(got) != 1 || got[0].Host != "blog.example.com" || got[0].appNames() != "shop, blog" {
		t.Errorf("after the last host: %+v", got)
	}
	// A host that another app shares stays.
	if _, err := rememberServer("vps-1", "shared", app("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := rememberServer("vps-1", "shared", app("b")); err != nil {
		t.Fatal(err)
	}
	if err := forgetServerApp("vps-1", app("b").Path); err != nil {
		t.Fatal(err)
	}
	if got, _ := readServers(); got[0].Host != "shared" {
		t.Errorf("a shared host: %+v", got)
	}
	for _, a := range []string{"a", "blog", "shop"} {
		if err := forgetServerApp("vps-1", app(a).Path); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := readServers(); len(got) != 0 {
		t.Errorf("after the last: %+v", got)
	}
	if text := readFileText(t, filepath.Join(dir, "config.yaml")); !strings.Contains(text, "servers: []") || !strings.Contains(text, "# Servers") {
		t.Errorf("file:\n%s", text)
	}
	// Nothing to forget is not an error, and a missing file is not made.
	if err := forgetServerApp("gone", "/x"); err != nil {
		t.Error(err)
	}
	other := homeOf(t)
	if err := forgetServerApp("vps-1", "/x"); err != nil || exists(filepath.Join(other, "config.yaml")) {
		t.Errorf("forget without a file: %v", err)
	}
}

// A file that is not what tifa wrote is an error that names it, and is left alone. A server listed
// twice names its id.
func TestServersBadFile(t *testing.T) {
	dir := homeOf(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	twice := "servers:\n  - {id: vps-1, host: a, apps: [{name: x, path: /x, host: a}]}\n  - {id: vps-1, host: b, apps: [{name: y, path: /y, host: b}]}\n"
	for _, bad := range []string{"servers: [", "servers: 3\n", "servers:\n  - host: x\n", "- a\n", twice, "servers:\n  - {id: a, host: b, apps: [shop]}\n"} {
		writeFile(t, path, bad)
		if _, err := readServers(); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%q read: %v", bad, err)
		}
		if _, err := rememberServer("vps-9", "vps", app("shop")); err == nil {
			t.Errorf("%q written over", bad)
		}
		if readFileText(t, path) != bad {
			t.Errorf("%q changed", bad)
		}
	}
	writeFile(t, path, twice)
	if _, err := readServers(); err == nil || !strings.Contains(err.Error(), "vps-1") || !strings.Contains(err.Error(), "twice") {
		t.Errorf("twice: %v", err)
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
			if _, err := rememberServer("big-1", "big", app(fmt.Sprintf("app%d", i))); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := readServers()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.ID == "big-1" && len(s.Apps) != 12 {
			t.Errorf("%d apps: %+v", len(s.Apps), s.Apps)
		}
	}
}

// The server of an app: the one that lists its folder, else the only one, else none.
func TestPickServer(t *testing.T) {
	a := func(name, host string) knownApp { k := app(name); k.Host = host; return k }
	one := []knownServer{{ID: "vps-1", Host: "h1", Apps: []knownApp{a("blog", "h1")}}}
	two := append(slices.Clone(one), knownServer{ID: "hz-2", Host: "h2b", Apps: []knownApp{a("shop", "h2")}})
	if _, _, ok := pickServer(nil, app("shop").Path); ok {
		t.Error("none")
	}
	if s, host, ok := pickServer(one, app("shop").Path); !ok || s.ID != "vps-1" || host != "h1" {
		t.Errorf("the only one: %+v %q", s, host)
	}
	if s, host, ok := pickServer(two, app("shop").Path); !ok || s.ID != "hz-2" || host != "h2" {
		t.Errorf("the one with the app uses the host of the app: %+v %q", s, host)
	}
	if _, _, ok := pickServer(two, app("notes").Path); ok {
		t.Error("two servers, and the folder is in neither")
	}
	both := []knownServer{two[1], {ID: "x-3", Host: "h3", Apps: []knownApp{a("shop", "h3")}}}
	if _, _, ok := pickServer(both, app("shop").Path); ok {
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

// at is the app of the root folder of a test.
func at(root, name string) knownApp { return knownApp{Name: name, Path: root} }

// The server comes from the flag, the environment and .env before ~/.tifa. With no server named,
// the only server of ~/.tifa is used, and a line says so and what to write to choose.
func TestDeployHostFromHome(t *testing.T) {
	homeOf(t)
	root := shopApp(t)
	if _, err := rememberServer("vps-1a2b3c4d", "ssh://203.0.113.7:22", app("blog")); err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	d, err := loadDeployTarget(root, "", &said)
	if err != nil || d.host != "ssh://203.0.113.7:22" || d.dest.port != 22 || d.dest.knownID != "vps-1a2b3c4d" {
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
	if d, err := loadDeployTarget(root, "from-flag", &said); err != nil || d.host != "from-flag" || said.Len() != 0 || d.dest.knownID != "" {
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

// In GitHub Actions the file is never read: no host is an error about the secret.
func TestDeployHostInActionsNeverReadsHome(t *testing.T) {
	homeOf(t)
	root := shopApp(t)
	if _, err := rememberServer("vps-1", "ssh://203.0.113.7:22", at(root, "shop")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envGitHubActions, "true")
	_, err := loadTarget(root, "")
	if err == nil || !strings.Contains(err.Error(), "secret TIFA_DEPLOY_HOST is missing") {
		t.Errorf("got %v", err)
	}
	if d, err := loadTarget(root, "ssh://h:22"); err != nil || d.host != "ssh://h:22" {
		t.Errorf("the flag works: %+v, %v", d, err)
	}
}

// Another checkout of an app with the same name is a new app: it is not matched by its name.
func TestDeployHostAppsAreFolders(t *testing.T) {
	homeOf(t)
	root := shopApp(t)
	other := filepath.Join(t.TempDir(), "other", "shop")
	writeFile(t, filepath.Join(other, "go.mod"), "module example.com/shop\n")
	for _, s := range []struct {
		id, host string
		app      knownApp
	}{
		{"vps-1", "ssh://203.0.113.7:22", at(root, "shop")},
		{"hz-2", "ssh://198.51.100.4:22", app("notes")},
	} {
		if _, err := rememberServer(s.id, s.host, s.app); err != nil {
			t.Fatal(err)
		}
	}
	if d, err := loadTarget(root, ""); err != nil || d.host != "ssh://203.0.113.7:22" {
		t.Errorf("the checkout that was set up: %+v, %v", d, err)
	}
	if _, err := loadTarget(other, ""); err == nil || !strings.Contains(err.Error(), "lists several") {
		t.Errorf("a checkout of the same name: %v", err)
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
		if _, err := rememberServer(s.id, s.host, app(s.app)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := loadTarget(root, "")
	if err == nil {
		t.Fatal("two servers, none named, and no error")
	}
	for _, want := range []string{"shop", "vps", "ssh://203.0.113.7:22", "apps: blog", "hz", "vps-ssh-k3j9.example.com", "apps: notes", "TIFA_DEPLOY_HOST=<host>", ".env", "--host"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%s", want, err)
		}
	}
	if strings.Contains(err.Error(), "TIFA_DEPLOY_HOST=ssh://") {
		t.Errorf("the example line is one of the servers:\n%s", err)
	}
	// An app that setup put on one of them goes there, without a question, by its own host.
	if _, err := rememberServer("hz-99998888", "vps-ssh-shop.example.com", at(root, "shop")); err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	d, err := loadDeployTarget(root, "", &said)
	if err != nil || d.host != "vps-ssh-shop.example.com" || !strings.Contains(said.String(), "shop is set up on hz") {
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

// A host from ~/.tifa that now answers as another machine stops the command, with both IDs and
// the host. The check is the probe the command makes anyway. A host that was named is not checked.
func TestDeployHostChecksTheMachine(t *testing.T) {
	homeOf(t)
	root := shopApp(t)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	if _, err := rememberServer("old-99999999", "vps", at(root, "shop")); err != nil {
		t.Fatal(err)
	}
	d, err := loadTarget(root, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.state(context.Background(), io.Discard)
	if err == nil {
		t.Fatal("another machine, and no error")
	}
	for _, want := range []string{"vps", "old-99999999", "vps-1234", "config.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%s", want, err)
		}
	}
	if _, err := rememberServer("vps-1234", "vps", at(root, "shop")); err != nil {
		t.Fatal(err)
	}
	d, _ = loadTarget(root, "")
	if _, err := d.state(context.Background(), io.Discard); err != nil {
		t.Errorf("the same machine: %v", err)
	}
	named, _ := loadTarget(root, "vps")
	if _, err := named.state(context.Background(), io.Discard); err != nil {
		t.Errorf("a named host: %v", err)
	}
}
