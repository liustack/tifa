package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

const restToken = "github_pat_test_token_value"

// installREST makes the fake stand for the REST API of GitHub: no gh, a token, and a server that
// keeps the secrets (opened with its private key, so the maps hold what CI would get) and the
// variables of g.
func (g *fakeGH) installREST(t *testing.T) {
	t.Helper()
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	owner, name, _ := strings.Cut(g.repo, "/")
	base := "/repos/" + g.repo
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fakeMu.Lock()
		defer fakeMu.Unlock()
		g.calls = append(g.calls, r.Method+" "+r.URL.Path)
		reply := func(status int, v any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			if v != nil {
				json.NewEncoder(w).Encode(v)
			}
		}
		if r.Header.Get("Authorization") != "Bearer "+restToken {
			reply(401, map[string]string{"message": "Bad credentials"})
			return
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			reply(415, map[string]string{"message": "wrong headers"})
			return
		}
		var body map[string]string
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			if len(data) > 0 {
				if err := json.Unmarshal(data, &body); err != nil {
					reply(400, map[string]string{"message": "bad json"})
					return
				}
			}
		}
		path := r.URL.Path
		switch {
		case r.Method == "GET" && path == base:
			if g.branch == "" {
				reply(200, map[string]any{"full_name": g.repo, "default_branch": ""})
				return
			}
			reply(200, map[string]any{"full_name": g.repo, "default_branch": g.branch, "owner": map[string]string{"login": owner}, "name": name})
		case r.Method == "GET" && path == base+"/actions/secrets/public-key":
			reply(200, map[string]string{"key_id": "key-1", "key": base64.StdEncoding.EncodeToString(pub[:])})
		case r.Method == "GET" && path == base+"/actions/secrets":
			var names []string
			for n := range g.secrets {
				names = append(names, n)
			}
			sort.Strings(names)
			var list []map[string]string
			for _, n := range pageOf(r, names, 100) {
				list = append(list, map[string]string{"name": n})
			}
			reply(200, map[string]any{"total_count": len(names), "secrets": list})
		case r.Method == "PUT" && strings.HasPrefix(path, base+"/actions/secrets/"):
			sealed, err := base64.StdEncoding.DecodeString(body["encrypted_value"])
			plain, ok := box.OpenAnonymous(nil, sealed, pub, priv)
			if err != nil || !ok || body["key_id"] != "key-1" {
				reply(422, map[string]string{"message": "cannot open the value"})
				return
			}
			n := strings.TrimPrefix(path, base+"/actions/secrets/")
			_, had := g.secrets[n]
			g.secrets[n] = string(plain)
			reply(map[bool]int{true: 204, false: 201}[had], nil)
		case r.Method == "DELETE" && strings.HasPrefix(path, base+"/actions/secrets/"):
			n := strings.TrimPrefix(path, base+"/actions/secrets/")
			if _, ok := g.secrets[n]; !ok {
				reply(404, map[string]string{"message": "Not Found"})
				return
			}
			delete(g.secrets, n)
			reply(204, nil)
		case r.Method == "GET" && path == base+"/actions/variables":
			var names []string
			for n := range g.vars {
				names = append(names, n)
			}
			sort.Strings(names)
			var list []map[string]string
			for _, n := range pageOf(r, names, 30) {
				list = append(list, map[string]string{"name": n, "value": g.vars[n]})
			}
			reply(200, map[string]any{"total_count": len(names), "variables": list})
		case r.Method == "POST" && path == base+"/actions/variables":
			if _, ok := g.vars[body["name"]]; ok {
				reply(409, map[string]string{"message": "Already exists"})
				return
			}
			g.vars[body["name"]] = body["value"]
			reply(201, nil)
		case r.Method == "PATCH" && strings.HasPrefix(path, base+"/actions/variables/"):
			n := strings.TrimPrefix(path, base+"/actions/variables/")
			if _, ok := g.vars[n]; !ok {
				reply(404, map[string]string{"message": "Not Found"})
				return
			}
			g.vars[n] = body["value"]
			reply(204, nil)
		case r.Method == "DELETE" && strings.HasPrefix(path, base+"/actions/variables/"):
			n := strings.TrimPrefix(path, base+"/actions/variables/")
			if _, ok := g.vars[n]; !ok {
				reply(404, map[string]string{"message": "Not Found"})
				return
			}
			delete(g.vars, n)
			reply(204, nil)
		default:
			reply(404, map[string]string{"message": "the fake does not know " + r.Method + " " + path})
		}
	}))
	oldBase, oldRun, oldHave, oldRemote := githubAPIBase, runGH, haveGH, githubRemoteRepo
	t.Cleanup(func() {
		srv.Close()
		githubAPIBase, runGH, haveGH, githubRemoteRepo = oldBase, oldRun, oldHave, oldRemote
	})
	githubAPIBase = srv.URL
	haveGH = func() bool { return false }
	runGH = func(_ context.Context, _ string, _ io.Reader, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("gh ran with %v, but this backend is the REST API", args)
	}
	githubRemoteRepo = func(context.Context, string) (string, error) { return g.repo, nil }
	t.Setenv(githubTokenEnv, restToken)
}

func pageOf(r *http.Request, names []string, max int) []string {
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per <= 0 || per > max {
		per = max
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max2(page, 1)
	lo := min((page-1)*per, len(names))
	return names[lo:min(lo+per, len(names))]
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// installBackend installs the fake as gh, or as the REST API.
func (g *fakeGH) installBackend(t *testing.T, rest bool) {
	if rest {
		g.installREST(t)
		return
	}
	g.install(t)
}

// eachBackend runs f once with gh and once with the REST API behind the same fake.
func eachBackend(t *testing.T, f func(t *testing.T, rest bool)) {
	for _, rest := range []bool{false, true} {
		t.Run(map[bool]string{false: "gh", true: "rest"}[rest], func(t *testing.T) { f(t, rest) })
	}
}

// isWriteCall says a call of the fake changed something.
func isWriteCall(c string) bool {
	return strings.Contains(c, " set ") || strings.Contains(c, " delete ") ||
		strings.HasPrefix(c, "PUT ") || strings.HasPrefix(c, "POST ") || strings.HasPrefix(c, "PATCH ") || strings.HasPrefix(c, "DELETE ")
}

func newTestREST(t *testing.T) (*githubREST, *fakeGH) {
	g := newFakeGH()
	g.installREST(t)
	return &githubREST{token: restToken, owner: "acme", name: "shop"}, g
}

// A secret reaches GitHub sealed with the public key of the repository, and only the private key
// of that repository opens it. Nothing else carries the value.
func TestRESTSecretIsSealed(t *testing.T) {
	api, g := newTestREST(t)
	ctx := context.Background()
	if err := api.SetSecret(ctx, "acme/shop", "TIFA_DEPLOY_SSH_KEY", "-----BEGIN KEY-----\nabc"); err != nil {
		t.Fatal(err)
	}
	if g.secrets["TIFA_DEPLOY_SSH_KEY"] != "-----BEGIN KEY-----\nabc" {
		t.Errorf("secrets %v", g.secrets)
	}
	if err := api.SetSecret(ctx, "acme/shop", "TIFA_DEPLOY_SSH_KEY", "second"); err != nil || g.secrets["TIFA_DEPLOY_SSH_KEY"] != "second" {
		t.Errorf("replace: %v %v", err, g.secrets)
	}
	names, err := api.SecretNames(ctx, "acme/shop")
	if err != nil || len(names) != 1 || !names["TIFA_DEPLOY_SSH_KEY"] {
		t.Errorf("names %v %v", names, err)
	}
	if err := api.DeleteSecret(ctx, "acme/shop", "TIFA_DEPLOY_SSH_KEY"); err != nil || len(g.secrets) != 0 {
		t.Errorf("delete: %v %v", err, g.secrets)
	}
	// A secret that is gone already is gone.
	if err := api.DeleteSecret(ctx, "acme/shop", "TIFA_DEPLOY_SSH_KEY"); err != nil {
		t.Errorf("delete again: %v", err)
	}
}

// Variables: set makes or replaces, the list pages through, delete removes.
func TestRESTVariables(t *testing.T) {
	api, g := newTestREST(t)
	ctx := context.Background()
	for i := range 35 {
		g.vars[fmt.Sprintf("V%02d", i)] = "x"
	}
	if err := api.SetVariable(ctx, "acme/shop", "TIFA_DEPLOY_FINGERPRINT", "one"); err != nil {
		t.Fatal(err)
	}
	if err := api.SetVariable(ctx, "acme/shop", "TIFA_DEPLOY_FINGERPRINT", "two"); err != nil {
		t.Fatal(err)
	}
	got, err := api.Variables(ctx, "acme/shop")
	if err != nil || len(got) != 36 || got["TIFA_DEPLOY_FINGERPRINT"] != "two" {
		t.Fatalf("%d variables, %v", len(got), err)
	}
	if err := api.DeleteVariable(ctx, "acme/shop", "TIFA_DEPLOY_FINGERPRINT"); err != nil || len(g.vars) != 35 {
		t.Errorf("delete: %v", err)
	}
}

// The repository and its default branch come from the git remote and the API.
func TestRESTRepo(t *testing.T) {
	api, g := newTestREST(t)
	repo, err := api.Repo(context.Background())
	if err != nil || repo != (githubRepo{Name: "acme/shop", Branch: "main"}) {
		t.Fatalf("%+v, %v", repo, err)
	}
	g.branch = ""
	if _, err := api.Repo(context.Background()); err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Errorf("no branch: %v", err)
	}
}

// A refused token says so and says which permissions it needs. The token is in no message.
func TestRESTErrorsNameThePermissionsAndHideTheToken(t *testing.T) {
	newTestREST(t)
	bad := &githubREST{token: "github_pat_wrong", owner: "acme", name: "shop"}
	_, err := bad.SecretNames(context.Background(), "acme/shop")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), githubTokenEnv) || strings.Contains(err.Error(), "github_pat_wrong") {
		t.Errorf("401: %v", err)
	}
	// A repository the token cannot see answers 404.
	ok := &githubREST{token: restToken, owner: "acme", name: "shop"}
	_, err = ok.SecretNames(context.Background(), "acme/other")
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "Secrets") || strings.Contains(err.Error(), restToken) {
		t.Errorf("404: %v", err)
	}
}

func TestParseGitHubRemote(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/shop.git":           "acme/shop",
		"https://github.com/acme/shop":               "acme/shop",
		"https://user:pw@github.com/acme/shop.git":   "acme/shop",
		"git@github.com:acme/shop.git":               "acme/shop",
		"ssh://git@github.com/acme/shop.git":         "acme/shop",
		"ssh://git@ssh.github.com:443/acme/shop.git": "acme/shop",
		"https://example.com/acme/shop.git":          "",
		"git@gitlab.com:acme/shop.git":               "",
		"https://github.com/acme":                    "",
	} {
		if got := parseGitHubRemote(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// The real git: origin first, then any other remote that is on github.com.
func TestGitHubRemoteRepoFromGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if _, err := githubRemoteRepo(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "remote") {
		t.Errorf("no remote: %v", err)
	}
	run("remote", "add", "up", "https://gitlab.com/x/y.git")
	run("remote", "add", "fork", "git@github.com:me/shop.git")
	if got, err := githubRemoteRepo(context.Background(), dir); err != nil || got != "me/shop" {
		t.Errorf("fork: %q, %v", got, err)
	}
	run("remote", "add", "origin", "https://github.com/acme/shop.git")
	if got, err := githubRemoteRepo(context.Background(), dir); err != nil || got != "acme/shop" {
		t.Errorf("origin: %q, %v", got, err)
	}
}

// gh comes first: when it is installed and logged in, the token is not used.
func TestGitHubPrefersGH(t *testing.T) {
	g := newFakeGH()
	g.install(t)
	t.Setenv(githubTokenEnv, restToken)
	api, repo, err := findGitHub(context.Background(), t.TempDir())
	if err != nil || repo.Name != "acme/shop" {
		t.Fatalf("%+v %v", repo, err)
	}
	if _, ok := api.(ghCLI); !ok {
		t.Errorf("backend %T", api)
	}
	// gh not logged in: the token takes over.
	g.loggedIn = false
	g2 := newFakeGH()
	g2.installREST(t)
	api, _, err = findGitHub(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := api.(*githubREST); !ok {
		t.Errorf("backend %T", api)
	}
}

// The token can sit in .env like the other inputs.
func TestGitHubTokenFromEnvFile(t *testing.T) {
	g := newFakeGH()
	g.installREST(t)
	t.Setenv(githubTokenEnv, "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".env"), githubTokenEnv+"="+restToken+"\n")
	if _, repo, err := findGitHub(context.Background(), root); err != nil || repo.Name != "acme/shop" {
		t.Errorf("%+v %v", repo, err)
	}
}

// Neither gh nor a token: the error says the two ways, with the exact settings of the token and
// the link that fills in the form of GitHub.
func TestGitHubNeitherGHNorToken(t *testing.T) {
	g := newFakeGH()
	g.loggedIn = false
	g.install(t)
	t.Setenv(githubTokenEnv, "")
	oldRemote := githubRemoteRepo
	t.Cleanup(func() { githubRemoteRepo = oldRemote })
	githubRemoteRepo = func(context.Context, string) (string, error) { return "acme/shop", nil }
	_, _, err := findGitHub(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{
		"gh is not logged in: run gh auth login",
		githubTokenEnv + "=",
		".env",
		"https://github.com/settings/personal-access-tokens/new?",
		"target_name=acme",
		"secrets=write",
		"actions_variables=write",
		"metadata=read",
		"Only select repositories",
		"acme/shop",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	// No gh program at all.
	haveGH = func() bool { return false }
	_, _, err = findGitHub(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not installed") || !strings.Contains(err.Error(), githubTokenEnv) {
		t.Errorf("no gh: %v", err)
	}
}

// The link is the plain form when the repository is not known, and fits the 40 characters of a name.
func TestGitHubTokenLink(t *testing.T) {
	if got := githubTokenLink(""); !strings.HasPrefix(got, "https://github.com/settings/personal-access-tokens/new?") || strings.Contains(got, "target_name") {
		t.Errorf("no repo: %s", got)
	}
	long := githubTokenLink("an-owner-with-a-long-name/and-a-long-repository-name")
	if i := strings.Index(long, "name="); i < 0 {
		t.Fatalf("%s", long)
	}
	u := strings.SplitN(strings.SplitN(long, "name=", 2)[1], "&", 2)[0]
	if dec := strings.ReplaceAll(strings.ReplaceAll(u, "%20", " "), "%2F", "/"); len(dec) > 40 {
		t.Errorf("name %q is %d characters", dec, len(dec))
	}
}

// With a token and no gh, setup works the same: the setup and remove tests also run on the REST backend.
func TestSetupNoGHWithToken(t *testing.T) {
	root := setupApp(t, "deploy:\n  auto: {}\n", nil)
	newSetupServer(t, "hostname 203.0.113.7\nport 2222\n", factsOf("shop", 9531, ""))
	g := newFakeGH()
	g.installREST(t)
	out, err := runSetup(t, root, false, "", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if g.secrets[secretHost] != "ssh://203.0.113.7:2222" || g.vars[varMarker] == "" {
		t.Errorf("secrets %v vars %v", g.secrets, g.vars)
	}
	for _, c := range g.calls {
		if strings.Contains(c, restToken) {
			t.Errorf("token in a call: %s", c)
		}
	}
	if strings.Contains(out, restToken) {
		t.Errorf("token in the output:\n%s", out)
	}
}
