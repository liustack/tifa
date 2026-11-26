package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeCloudflareAPI answers the purge request and keeps what it got.
type fakeCloudflareAPI struct {
	status int
	path   string
	auth   string
	body   string
}

func (f *fakeCloudflareAPI) install(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		f.path, f.auth, f.body = r.Method+" "+r.URL.Path, r.Header.Get("Authorization"), string(data)
		w.WriteHeader(f.status)
		if f.status == 200 {
			io.WriteString(w, `{"success":true,"errors":[]}`)
		} else {
			io.WriteString(w, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`)
		}
	}))
	old := cloudflareAPI
	t.Cleanup(func() { srv.Close(); cloudflareAPI = old })
	cloudflareAPI = srv.URL
}

// CI clears the cache by host name only, through the API with the token of the environment, and
// never the whole zone.
func TestPurgeRequestShape(t *testing.T) {
	api := &fakeCloudflareAPI{status: 200}
	api.install(t)
	t.Setenv(envPurgeToken, "tok-123")
	t.Setenv(envZoneID, "zone-9")
	r := clearCache(context.Background(), purgeTarget(t.TempDir(), "shop.example.com"), io.Discard)
	if !r.Cleared || r.Warning != "" {
		t.Fatalf("result %+v", r)
	}
	if api.path != "POST /zones/zone-9/purge_cache" || api.auth != "Bearer tok-123" {
		t.Errorf("request %s, auth %q", api.path, api.auth)
	}
	if api.body != `{"hosts":["shop.example.com"]}` || strings.Contains(api.body, "purge_everything") {
		t.Errorf("body %s", api.body)
	}
}

// A purge that fails warns and names the reason, without the token. No domain: no request.
func TestPurgeFailureIsAWarning(t *testing.T) {
	api := &fakeCloudflareAPI{status: 403}
	api.install(t)
	t.Setenv(envPurgeToken, "tok-secret")
	t.Setenv(envZoneID, "zone-9")
	r := clearCache(context.Background(), purgeTarget(t.TempDir(), "shop.example.com"), io.Discard)
	if r.Cleared || !strings.Contains(r.Warning, "403") || !strings.Contains(r.Warning, "Authentication error") || strings.Contains(r.Warning, "tok-secret") {
		t.Errorf("result %+v", r)
	}
	api.path = ""
	if r := clearCache(context.Background(), purgeTarget(t.TempDir(), ""), io.Discard); r.Cleared || api.path != "" || r.String() != "not used, deploy.front is not cloudflare" {
		t.Errorf("no domain: %+v, %q", r, api.path)
	}
	t.Setenv(envZoneID, "")
	if r := clearCache(context.Background(), purgeTarget(t.TempDir(), "shop.example.com"), io.Discard); r.Cleared || !strings.Contains(r.Warning, envZoneID) {
		t.Errorf("no zone: %+v", r)
	}
}

// Without a token or a cf login the cache is left as it is. That is the choice of the developer, so
// the line says what it costs and is no warning.
func TestNoPurgeTokenIsNoWarning(t *testing.T) {
	t.Setenv(envPurgeToken, "")
	useRecordingSSH(t) // a server without the file of tokens: it prints nothing
	oldHave := haveCF
	t.Cleanup(func() { haveCF = oldHave })
	haveCF = func() bool { return false }
	r := clearCache(context.Background(), purgeTarget(t.TempDir(), "shop.example.com"), io.Discard)
	if r.Cleared || !r.NoToken || r.Warning != "" {
		t.Fatalf("result %+v", r)
	}
	var out strings.Builder
	printCache(&out, r)
	if got := out.String(); strings.Contains(got, "warning") || !strings.Contains(got, "cache: not cleared for shop.example.com") || !strings.Contains(got, "Cache time") || !strings.Contains(got, "tifa deploy setup --cache-purge-token") {
		t.Errorf("printed %q", got)
	}
	md := deploySummary{App: "shop", Action: "deploy", Cache: r}.markdown()
	if strings.Contains(md, "Warning") || !strings.Contains(md, "| CDN cache | not cleared for shop.example.com") {
		t.Errorf("summary:\n%s", md)
	}
}

// On a machine with cf logged in, cf clears the cache of the host name.
func TestPurgeWithCF(t *testing.T) {
	t.Setenv(envPurgeToken, "")
	useRecordingSSH(t)
	f := &fakeAccess{fakeCF: newFakeCF("example.com")}
	f.install(t)
	inner := runCF
	runCF = func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "cache" {
			fakeMu.Lock()
			defer fakeMu.Unlock()
			f.calls = append(f.calls, strings.Join(args, " "))
			return []byte(`{}`), nil
		}
		return inner(ctx, args...)
	}
	r := clearCache(context.Background(), purgeTarget(t.TempDir(), "shop.example.com"), io.Discard)
	if !r.Cleared {
		t.Fatalf("%+v", r)
	}
	want := `cache purge -z example.com --force --body {"hosts":["shop.example.com"]}`
	if got := f.calls[len(f.calls)-1]; got != want {
		t.Errorf("call %q, want %q", got, want)
	}
}

// A deploy whose purge fails still succeeds: a warning, exit 0.
func TestDeployGoesThroughWhenThePurgeFails(t *testing.T) {
	api := &fakeCloudflareAPI{status: 403}
	api.install(t)
	t.Setenv(envPurgeToken, "tok")
	t.Setenv(envZoneID, "zone-9")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n")
	t.Setenv(deployHostEnv, "vps")
	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv(envStepSummary, summary)
	home := t.TempDir()
	prepareFakeServer(t, home)
	fakeServer(t, home, nil)
	oldNow, oldBuild := deployNow, buildForServer
	t.Cleanup(func() { deployNow, buildForServer = oldNow, oldBuild })
	deployNow = func() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) }
	buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
		return os.WriteFile(out, []byte("program"), 0o755)
	}
	out, _, err := run(t, "deploy", "--dir", root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "warning: the CDN cache of shop.example.com was not cleared") || strings.Contains(out, "tok") {
		t.Errorf("output:\n%s", out)
	}
	data, _ := os.ReadFile(summary)
	for _, want := range []string{"### Deploy of shop done", "| Release | 20261120T100000Z |", "| Health check | GET /healthz answered 404 |", "NOT cleared for shop.example.com", "Warning:"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("summary lacks %q:\n%s", want, data)
		}
	}
}

// The summary of a run of Actions: written when GITHUB_STEP_SUMMARY names a file, and nothing
// otherwise. A rollback and a failure say so.
func TestStepSummary(t *testing.T) {
	t.Setenv(envStepSummary, "")
	if err := writeStepSummary(deploySummary{App: "shop"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "s.md")
	t.Setenv(envStepSummary, path)
	ok := deploySummary{App: "shop", Action: "rollback", Release: "r1", Commit: "abc", Elapsed: 2 * time.Second,
		Health: "GET /healthz answered 200", Cache: cacheResult{Host: "shop.example.com", Cleared: true}}
	failed := deploySummary{App: "shop", Action: "deploy", Err: failedRun{}}
	for _, s := range []deploySummary{ok, failed} {
		if err := writeStepSummary(s); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{"### Rollback of shop done", "| Commit | abc |", "| Time | 2s |", "| CDN cache | cleared for shop.example.com |", "### Deploy of shop failed", "| Error | release r2 failed |"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lacks %q:\n%s", want, data)
		}
	}
}

type failedRun struct{}

func (failedRun) Error() string { return "release r2 failed" }

// The health check line of the server is kept as it goes by.
func TestHealthCapture(t *testing.T) {
	var out strings.Builder
	h := &healthCapture{w: &out}
	io.WriteString(h, "starting r\nhealth che")
	io.WriteString(h, "ck: GET /healthz answered 200\ndone\n")
	if h.line != "GET /healthz answered 200" || !strings.Contains(out.String(), "starting r") {
		t.Errorf("line %q, out %q", h.line, out.String())
	}
}

// The token and the zone ID are inputs like the server: the environment first, else the .env at the
// root of the app. A machine without a cf login clears the cache with them.
func TestPurgeInputsFromEnvFile(t *testing.T) {
	api := &fakeCloudflareAPI{status: 200}
	api.install(t)
	t.Setenv(envPurgeToken, "")
	t.Setenv(envZoneID, "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".env"), "OTHER=1\n"+envPurgeToken+"=tok-file\n"+envZoneID+"=zone-file\n")
	r := clearCache(context.Background(), purgeTarget(root, "shop.example.com"), io.Discard)
	if !r.Cleared || api.path != "POST /zones/zone-file/purge_cache" || api.auth != "Bearer tok-file" {
		t.Fatalf(".env: %+v, %s, %q", r, api.path, api.auth)
	}
	// The environment beats the file, for each input on its own.
	t.Setenv(envZoneID, "zone-env")
	if r := clearCache(context.Background(), purgeTarget(root, "shop.example.com"), io.Discard); !r.Cleared || api.path != "POST /zones/zone-env/purge_cache" || api.auth != "Bearer tok-file" {
		t.Errorf("env zone: %+v, %s, %q", r, api.path, api.auth)
	}
	// A token without a zone ID: a purge-only token cannot read the zone, so the warning says to give it.
	writeFile(t, filepath.Join(root, ".env"), envPurgeToken+"=tok-file\n")
	t.Setenv(envZoneID, "")
	r = clearCache(context.Background(), purgeTarget(root, "shop.example.com"), io.Discard)
	if r.Cleared || !strings.Contains(r.Warning, envZoneID) || !strings.Contains(r.Warning, ".env") || strings.Contains(r.Warning, "tok-file") {
		t.Errorf("no zone: %+v", r)
	}
	// A .env that cannot be read is a warning, not a silent skip.
	writeFile(t, filepath.Join(root, ".env"), "not a setting\n")
	if r := clearCache(context.Background(), purgeTarget(root, "shop.example.com"), io.Discard); r.Cleared || r.NoToken || !strings.Contains(r.Warning, ".env") {
		t.Errorf("bad .env: %+v", r)
	}
}

// purgeTarget is the app shop on the server vps with the domain of deploy.front cloudflare, or
// none.
func purgeTarget(root, domain string) deployTarget {
	return deployTarget{root: root, app: "shop", host: "vps", domain: domain}
}

// writePurgeFile puts the file of tokens that setup keeps into the home of a fake server.
func writePurgeFile(t *testing.T, home, text string) {
	t.Helper()
	writeFile(t, filepath.Join(home, filepath.FromSlash(purgeFile)), text)
}

// noCF makes the machine one without cf.
func noCF(t *testing.T) {
	old := haveCF
	t.Cleanup(func() { haveCF = old })
	haveCF = func() bool { return false }
}

// Setup writes the file over ssh as tifa, the token on the input of ssh: the folder only tifa may
// enter, the file only tifa may read, a line per zone. A zone written again replaces its line, and
// the lines of the other zones stay. Deploy reads it back over ssh. With the last app it goes.
func TestPurgeFileOnTheServer(t *testing.T) {
	home := t.TempDir()
	calls := fakeServer(t, home, nil)
	tgt := purgeTarget(t.TempDir(), "shop.example.com")
	ctx := context.Background()
	if _, ok, err := tgt.serverPurge(ctx, io.Discard); ok || err != nil {
		t.Fatalf("no file: %v, %v", ok, err)
	}
	for _, c := range []purgeCred{{"example.com", "zone-old", "tok-old"}, {"example.org", "zone-org", "tok-org"}, {"example.com", "zone-com", "tok-com"}} {
		if err := tgt.storePurge(ctx, c, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(home, filepath.FromSlash(purgeFile))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), purgeFileHeader+"\nexample.org\tzone-org\ttok-org\nexample.com\tzone-com\ttok-com\n"; got != want {
		t.Errorf("file:\n%q\nwant\n%q", got, want)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(filepath.Dir(path)); st.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %v", st.Mode().Perm())
	}
	c, ok, err := tgt.serverPurge(ctx, io.Discard)
	if err != nil || !ok || c != (purgeCred{"example.com", "zone-com", "tok-com"}) {
		t.Errorf("read %+v, %v, %v", c, ok, err)
	}
	if strings.Contains(fakeServerArgv(), "tok-") {
		t.Errorf("a token is on a command line of ssh:\n%s", fakeServerArgv())
	}
	// Two setups on one server do not lose each other's line: each write holds the lock.
	if n := strings.Count(calls(), "flock -w 60 9"); n != 3 {
		t.Errorf("%d of 3 writes took the lock:\n%s", n, calls())
	}
	for _, bad := range []purgeCred{{"example.com", "z", "tok\tx"}, {"example.com", "z", "tok\nexample.org\tz\tt"}, {"example.com", "z", ""}, {"exa mple.com", "z", "t"}, {"example.com", "", "t"}} {
		if err := tgt.storePurge(ctx, bad, io.Discard); err == nil {
			t.Errorf("%q: stored", bad)
		}
	}
}

// The line of a domain is the one of its zone: the domain itself or the longest zone it ends in.
func TestPickPurgeLine(t *testing.T) {
	text := purgeFileHeader + "\nexample.org\tzone-org\ttok-org\nexample.com\tzone-com\ttok-com\nshop.example.com\tzone-sub\ttok-sub\n"
	for domain, want := range map[string]string{
		"example.com":        "zone-com",
		"www.example.com":    "zone-com",
		"shop.example.com":   "zone-sub",
		"a.shop.example.com": "zone-sub",
		"example.org":        "zone-org",
		"notexample.com":     "",
		"example.net":        "",
		// Host names are the same in any case.
		"www.SHOP.Example.com": "zone-sub",
		"EXAMPLE.ORG":          "zone-org",
	} {
		c, ok, err := pickPurgeLine(text, domain)
		if err != nil || ok != (want != "") || c.ZoneID != want {
			t.Errorf("%s: %+v, %v, %v", domain, c, ok, err)
		}
	}
	if _, _, err := pickPurgeLine("example.com\tzone-com\n", "example.com"); err == nil || strings.Contains(err.Error(), "zone-com") {
		t.Errorf("a broken line: %v", err)
	}
}

// Without the inputs, a deploy reads the token of the zone from the server and clears the cache
// with it. The inputs win over the file, the file wins over a cf login, and with none of them the
// line says what to run.
func TestPurgePrecedence(t *testing.T) {
	api := &fakeCloudflareAPI{status: 200}
	api.install(t)
	t.Setenv(envPurgeToken, "")
	t.Setenv(envZoneID, "")
	home := t.TempDir()
	calls := fakeServer(t, home, nil)
	writePurgeFile(t, home, purgeFileHeader+"\nexample.com\tzone-srv\ttok-srv\n")
	f := &fakeAccess{fakeCF: newFakeCF("example.com")}
	f.install(t)
	inner := runCF
	runCF = func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "cache" {
			fakeMu.Lock()
			defer fakeMu.Unlock()
			f.calls = append(f.calls, strings.Join(args, " "))
			return []byte(`{}`), nil
		}
		return inner(ctx, args...)
	}
	cfPurged := func() bool {
		return slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "cache purge") })
	}
	ctx := context.Background()
	tgt := purgeTarget(t.TempDir(), "shop.example.com")

	// 1. The environment, and the server is not asked.
	t.Setenv(envPurgeToken, "tok-env")
	t.Setenv(envZoneID, "zone-env")
	if r := clearCache(ctx, tgt, io.Discard); !r.Cleared || api.path != "POST /zones/zone-env/purge_cache" || api.auth != "Bearer tok-env" || cfPurged() || calls() != "" {
		t.Fatalf("environment: %+v, %s %q, cf %v, ssh %q", r, api.path, api.auth, f.calls, calls())
	}
	// 2. The file on the server, before the cf login.
	t.Setenv(envPurgeToken, "")
	t.Setenv(envZoneID, "")
	if r := clearCache(ctx, tgt, io.Discard); !r.Cleared || api.path != "POST /zones/zone-srv/purge_cache" || api.auth != "Bearer tok-srv" || cfPurged() {
		t.Fatalf("server: %+v, %s %q, cf %v", r, api.path, api.auth, f.calls)
	}
	if api.body != `{"hosts":["shop.example.com"]}` {
		t.Errorf("body %s", api.body)
	}
	// An empty zone ID input is no input: the server answers.
	t.Setenv(envZoneID, "")
	api.path = ""
	if r := clearCache(ctx, tgt, io.Discard); !r.Cleared || api.path != "POST /zones/zone-srv/purge_cache" {
		t.Fatalf("empty inputs: %+v, %s", r, api.path)
	}
	// 3. No line for the zone: the cf login.
	writePurgeFile(t, home, purgeFileHeader+"\nexample.org\tzone-org\ttok-org\n")
	api.path = ""
	if r := clearCache(ctx, tgt, io.Discard); !r.Cleared || api.path != "" || !cfPurged() {
		t.Fatalf("cf: %+v, %s, cf %v", r, api.path, f.calls)
	}
	// 4. None: not cleared, and the line says to run setup.
	noCF(t)
	r := clearCache(ctx, tgt, io.Discard)
	var out strings.Builder
	printCache(&out, r)
	if r.Cleared || !r.NoToken || !strings.Contains(out.String(), "run go tool tifa deploy setup --cache-purge-token") {
		t.Errorf("none: %+v\n%s", r, out.String())
	}
	if strings.Contains(fakeServerArgv(), "tok-") {
		t.Errorf("a token is on a command line of ssh:\n%s", fakeServerArgv())
	}
}

// A file the server cannot read back is a warning, and the deploy still goes through.
func TestPurgeServerFileBroken(t *testing.T) {
	t.Setenv(envPurgeToken, "")
	home := t.TempDir()
	fakeServer(t, home, nil)
	writePurgeFile(t, home, "example.com\tzone-srv\n")
	noCF(t)
	r := clearCache(context.Background(), purgeTarget(t.TempDir(), "shop.example.com"), io.Discard)
	if r.Cleared || r.NoToken || !strings.Contains(r.Warning, purgeFile) || strings.Contains(r.Warning, "zone-srv") {
		t.Errorf("%+v", r)
	}
}

// A deploy and a rollback run locally or in CI read the token from the server once the release
// is live, and clear the cache with it. The token shows in no output, no summary and no command
// line.
func TestDeployAndRollbackPurgeWithTheServerToken(t *testing.T) {
	api := &fakeCloudflareAPI{status: 200}
	api.install(t)
	t.Setenv(envPurgeToken, "")
	t.Setenv(envZoneID, "")
	noCF(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n")
	t.Setenv(deployHostEnv, "vps")
	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv(envStepSummary, summary)
	home := t.TempDir()
	prepareFakeServer(t, home)
	fakeServer(t, home, nil)
	writePurgeFile(t, home, purgeFileHeader+"\nexample.com\tzone-com\ttok-secret-com\n")
	clock := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	oldNow, oldBuild := deployNow, buildForServer
	t.Cleanup(func() { deployNow, buildForServer = oldNow, oldBuild })
	deployNow = func() time.Time { c := clock; clock = clock.Add(time.Hour); return c }
	buildForServer = func(_ context.Context, _, _, _, out string, _, _ io.Writer) error {
		return os.WriteFile(out, []byte("program"), 0o755)
	}
	var all strings.Builder
	for _, args := range [][]string{{"deploy", "--dir", root}, {"deploy", "--dir", root}, {"deploy", "rollback", "--dir", root}} {
		api.path = ""
		out, errOut, err := run(t, args...)
		all.WriteString(out + errOut)
		if err != nil {
			t.Fatalf("%v: %v\n%s%s", args, err, out, errOut)
		}
		if !strings.Contains(out, "cache: cleared for shop.example.com") || api.path != "POST /zones/zone-com/purge_cache" || api.auth != "Bearer tok-secret-com" {
			t.Errorf("%v: %s %q\n%s", args, api.path, api.auth, out)
		}
	}
	data, _ := os.ReadFile(summary)
	if !strings.Contains(string(data), "### Rollback of shop done") || !strings.Contains(string(data), "| CDN cache | cleared for shop.example.com |") {
		t.Errorf("summary:\n%s", data)
	}
	for what, text := range map[string]string{"output": all.String(), "summary": string(data), "ssh command lines": fakeServerArgv()} {
		if strings.Contains(text, "tok-secret") {
			t.Errorf("the token is in the %s:\n%s", what, text)
		}
	}
}

// Remove decides whether an app is left on the server under the lock of the file, in the script
// that deletes: an app that came since the plan keeps the file. With none left, the file, its lock
// and any temporary file a write left go, and the folder with them.
func TestPurgeForget(t *testing.T) {
	home := t.TempDir()
	calls := fakeServer(t, home, nil)
	tgt := purgeTarget(t.TempDir(), "")
	writePurgeFile(t, home, purgeFileHeader+"\nexample.com\tzone-com\ttok-com\n")
	left := filepath.Join(home, filepath.FromSlash(purgeFile)+".Ab12Cd")
	writeFile(t, left, "example.com\tzone-com\ttok-com\n")
	writeFile(t, filepath.Join(home, "notes", "port"), "9532\n")
	var out strings.Builder
	if err := tgt.ssh(context.Background(), deployUser, purgeForgetBody, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "kept") || !strings.Contains(out.String(), "notes") || !exists(left) || !strings.Contains(calls(), "flock -w 60 9") {
		t.Fatalf("with notes on the server: %q\n%s", out.String(), calls())
	}
	if err := os.RemoveAll(filepath.Join(home, "notes")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := tgt.ssh(context.Background(), deployUser, purgeForgetBody, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(home, filepath.FromSlash(purgeDir))) {
		entries, _ := os.ReadDir(filepath.Join(home, filepath.FromSlash(purgeDir)))
		t.Errorf("left %v: %q", entries, out.String())
	}
}

// A server file that cannot be read leaves the cf login to clear the cache, as before the file,
// and one line says the file could not be read. Without cf it is a warning.
func TestPurgeServerReadFails(t *testing.T) {
	t.Setenv(envPurgeToken, "")
	home := t.TempDir()
	fakeServer(t, home, nil)
	// A file that cannot be read: cat fails on it.
	writePurgeFile(t, home, purgeFileHeader+"\nexample.com\tzone-com\ttok-com\n")
	if err := os.Chmod(filepath.Join(home, filepath.FromSlash(purgeFile)), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(home, filepath.FromSlash(purgeFile)), 0o600) })
	f := &fakeAccess{fakeCF: newFakeCF("example.com")}
	f.install(t)
	inner := runCF
	runCF = func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "cache" {
			return []byte(`{}`), nil
		}
		return inner(ctx, args...)
	}
	tgt := purgeTarget(t.TempDir(), "shop.example.com")
	r := clearCache(context.Background(), tgt, io.Discard)
	var out strings.Builder
	printCache(&out, r)
	if !r.Cleared || strings.Count(out.String(), "\n") != 2 || !strings.Contains(out.String(), "could not be read") || !strings.Contains(out.String(), "cache: cleared for shop.example.com") {
		t.Errorf("with cf: %+v\n%s", r, out.String())
	}
	noCF(t)
	if r := clearCache(context.Background(), tgt, io.Discard); r.Cleared || r.NoToken || !strings.Contains(r.Warning, "could not be read") {
		t.Errorf("without cf: %+v", r)
	}
}
