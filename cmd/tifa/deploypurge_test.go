package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	r := clearCache(context.Background(), "shop.example.com")
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
	r := clearCache(context.Background(), "shop.example.com")
	if r.Cleared || !strings.Contains(r.Warning, "403") || !strings.Contains(r.Warning, "Authentication error") || strings.Contains(r.Warning, "tok-secret") {
		t.Errorf("result %+v", r)
	}
	api.path = ""
	if r := clearCache(context.Background(), ""); r.Cleared || api.path != "" || r.String() != "not used, deploy.cdn is not cloudflare" {
		t.Errorf("no domain: %+v, %q", r, api.path)
	}
	t.Setenv(envZoneID, "")
	if r := clearCache(context.Background(), "shop.example.com"); r.Cleared || !strings.Contains(r.Warning, envZoneID) {
		t.Errorf("no zone: %+v", r)
	}
}

// Without a token or a cf login the cache is left as it is. That is the choice of the developer, so
// the line says what it costs and is no warning.
func TestNoPurgeTokenIsNoWarning(t *testing.T) {
	t.Setenv(envPurgeToken, "")
	oldHave := haveCF
	t.Cleanup(func() { haveCF = oldHave })
	haveCF = func() bool { return false }
	r := clearCache(context.Background(), "shop.example.com")
	if r.Cleared || !r.NoToken || r.Warning != "" {
		t.Fatalf("result %+v", r)
	}
	var out strings.Builder
	printCache(&out, r)
	if got := out.String(); strings.Contains(got, "warning") || !strings.Contains(got, "cache: not cleared for shop.example.com") || !strings.Contains(got, "Cache time") || !strings.Contains(got, "tifa deploy setup --cdn-token") {
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
	r := clearCache(context.Background(), "shop.example.com")
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
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n")
	writeFile(t, filepath.Join(root, "tifa.yaml"), "site:\n  url: https://shop.example.com\ndeploy:\n  host: vps\n  cdn: cloudflare\n")
	t.Setenv(deployHostEnv, "")
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
