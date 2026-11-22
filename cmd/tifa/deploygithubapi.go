package main

// Every call setup and remove make to GitHub goes through githubAPI: reading the repository,
// listing its secrets and variables, and setting and deleting them. Two things implement it. The
// gh CLI is the first choice, when it is installed and logged in. Without it, the REST API with a
// fine-grained personal access token from the input TIFA_DEPLOY_GITHUB_TOKEN. The rest of the code
// does not care which one runs. The token is sent in the Authorization header only: never on a
// command line, never in a message.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/box"
)

// githubTokenEnv is the input that holds the fine-grained token: the environment, else the .env
// of the app. It is not a secret of the repository.
const githubTokenEnv = "TIFA_DEPLOY_GITHUB_TOKEN"

// githubAPIBase is the base of the REST API of GitHub. Tests point it at a fake.
var githubAPIBase = "https://api.github.com"

// githubAPI is what setup and remove need of GitHub. repo is owner/name.
type githubAPI interface {
	// Repo reads the repository of the app and its default branch.
	Repo(ctx context.Context) (githubRepo, error)
	SecretNames(ctx context.Context, repo string) (map[string]bool, error)
	Variables(ctx context.Context, repo string) (map[string]string, error)
	// SetSecret and SetVariable make the secret or variable, or replace it. A value is never
	// printed, and never put on a command line.
	SetSecret(ctx context.Context, repo, name, value string) error
	SetVariable(ctx context.Context, repo, name, value string) error
	// DeleteSecret and DeleteVariable leave nothing behind, and are no error when it is gone.
	DeleteSecret(ctx context.Context, repo, name string) error
	DeleteVariable(ctx context.Context, repo, name string) error
}

// findGitHub picks the way to talk to GitHub for the app at dir, and reads its repository. When
// neither works, the error says the two ways to make one work.
func findGitHub(ctx context.Context, dir string) (githubAPI, githubRepo, error) {
	ghWhy := ""
	switch {
	case !haveGH():
		ghWhy = "gh, the GitHub CLI, is not installed: " + ghInstallHow() + ", then run gh auth login"
	default:
		if err := ghAuthStatus(ctx, dir); err != nil {
			ghWhy = fmt.Sprintf("gh is not logged in: run gh auth login (%v)", err)
			break
		}
		api := ghCLI{dir: dir}
		repo, err := api.Repo(ctx)
		if err != nil {
			return nil, githubRepo{}, fmt.Errorf("%s is not the folder of a GitHub repository you can use: %w", dir, err)
		}
		return api, repo, nil
	}
	token, _, err := readDeployInput(dir, "", "", githubTokenEnv)
	if err != nil {
		return nil, githubRepo{}, err
	}
	name, remoteErr := githubRemoteRepo(ctx, dir)
	if token == "" {
		return nil, githubRepo{}, errors.New(ghWhy + ". Or without gh: " + githubTokenHelp(name))
	}
	if remoteErr != nil {
		return nil, githubRepo{}, remoteErr
	}
	owner, repoName, _ := strings.Cut(name, "/")
	api := &githubREST{token: token, owner: owner, name: repoName}
	repo, err := api.Repo(ctx)
	if err != nil {
		return nil, githubRepo{}, err
	}
	return api, repo, nil
}

// ghAuthStatus checks the account gh uses on github.com, and no other: a stale token of another
// account or host must not ask for a login. A gh too old to know --active is asked about the
// host alone.
func ghAuthStatus(ctx context.Context, dir string) error {
	_, err := readGH(ctx, dir, nil, "auth", "status", "--hostname", "github.com", "--active")
	if err != nil && strings.Contains(err.Error(), "unknown flag: --active") {
		_, err = readGH(ctx, dir, nil, "auth", "status", "--hostname", "github.com")
	}
	return err
}

// githubTokenHelp says how to make the token that stands in for gh. repo is owner/name, empty
// when the git remote does not say.
func githubTokenHelp(repo string) string {
	which := "this repository"
	if repo != "" {
		which = repo
	}
	return fmt.Sprintf("make a fine-grained personal access token. Open %s, which fills in the name and the permissions. "+
		"Under Repository access choose Only select repositories and pick %s. "+
		"Check that the repository permissions Secrets and Variables are Read and write (Metadata: Read comes with them). "+
		"Then put %s=<token> in the .env at the root of the app (git ignores it, and tifa only reads it)",
		githubTokenLink(repo), which, githubTokenEnv)
}

// githubTokenLink opens the form of a new fine-grained token with the name, the owner and the
// permissions filled in. GitHub cannot fill in the repository, so the text says to pick it.
// https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens
func githubTokenLink(repo string) string {
	esc := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	name := "tifa deploy"
	if n := name + " " + repo; repo != "" && len(n) <= 40 { // GitHub takes 40 characters
		name = n
	}
	q := "name=" + esc(name) + "&description=" + esc("Sets the secrets and the variable that tifa deploy setup needs")
	if owner, _, ok := strings.Cut(repo, "/"); ok {
		q += "&target_name=" + esc(owner)
	}
	// The repository permissions Secrets, Variables and Metadata, by the names of their parameters.
	q += "&secrets=write&actions_variables=write&metadata=read"
	return "https://github.com/settings/personal-access-tokens/new?" + q
}

// githubRemoteRepo reads owner/name of the GitHub repository from the git remotes of dir: origin
// first, then the first other remote on github.com. Tests replace it.
var githubRemoteRepo = func(ctx context.Context, dir string) (string, error) {
	c := exec.CommandContext(ctx, "git", "remote", "-v")
	c.Dir = dir
	var out, stderr bytes.Buffer
	c.Stdout, c.Stderr = &out, &stderr
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("git remote -v in %s: %s", dir, strings.TrimSpace(firstNonEmpty(stderr.String(), err.Error())))
	}
	other := ""
	for _, line := range strings.Split(out.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != "(fetch)" {
			continue
		}
		repo := parseGitHubRemote(fields[1])
		switch {
		case repo == "":
		case fields[0] == "origin":
			return repo, nil
		case other == "":
			other = repo
		}
	}
	if other == "" {
		return "", fmt.Errorf("%s has no git remote on github.com: add one with git remote add origin <url>", dir)
	}
	return other, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// parseGitHubRemote reads owner/name from the URL of a git remote on github.com, in the forms
// git uses: https, ssh:// and the scp-like git@github.com:owner/name. It gives "" for anything else.
func parseGitHubRemote(remote string) string {
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		host, path = u.Hostname(), u.Path
	} else if at := strings.Index(remote, "@"); at >= 0 {
		var ok bool
		if host, path, ok = strings.Cut(remote[at+1:], ":"); !ok {
			return ""
		}
	} else {
		return ""
	}
	if host != "github.com" && host != "ssh.github.com" {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if !repoName.MatchString(path) {
		return ""
	}
	return path
}

// ghCLI is githubAPI over the gh CLI, run in dir.
type ghCLI struct{ dir string }

func (g ghCLI) Repo(ctx context.Context) (githubRepo, error) {
	out, err := readGH(ctx, g.dir, nil, "repo", "view", "--json", "nameWithOwner,defaultBranchRef")
	if err != nil {
		return githubRepo{}, err
	}
	return parseRepoView(out)
}

func (g ghCLI) SecretNames(ctx context.Context, repo string) (map[string]bool, error) {
	out, err := readGH(ctx, g.dir, nil, "secret", "list", "--repo", repo, "--json", "name")
	if err != nil {
		return nil, err
	}
	var names []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &names); err != nil {
		return nil, fmt.Errorf("gh secret list printed what tifa cannot read: %w", err)
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n.Name] = true
	}
	return got, nil
}

func (g ghCLI) Variables(ctx context.Context, repo string) (map[string]string, error) {
	out, err := readGH(ctx, g.dir, nil, "variable", "list", "--repo", repo, "--json", "name,value")
	if err != nil {
		return nil, err
	}
	var vars []struct{ Name, Value string }
	if err := json.Unmarshal(out, &vars); err != nil {
		return nil, fmt.Errorf("gh variable list printed what tifa cannot read: %w", err)
	}
	got := map[string]string{}
	for _, v := range vars {
		got[v.Name] = v.Value
	}
	return got, nil
}

// The value goes to gh on its input.
func (g ghCLI) SetSecret(ctx context.Context, repo, name, value string) error {
	_, err := runGH(ctx, g.dir, strings.NewReader(value), "secret", "set", name, "--repo", repo)
	return err
}

func (g ghCLI) SetVariable(ctx context.Context, repo, name, value string) error {
	_, err := runGH(ctx, g.dir, strings.NewReader(value), "variable", "set", name, "--repo", repo)
	return err
}

func (g ghCLI) DeleteSecret(ctx context.Context, repo, name string) error {
	_, err := runGH(ctx, g.dir, nil, "secret", "delete", name, "--repo", repo)
	return err
}

func (g ghCLI) DeleteVariable(ctx context.Context, repo, name string) error {
	_, err := runGH(ctx, g.dir, nil, "variable", "delete", name, "--repo", repo)
	return err
}

// githubREST is githubAPI over the REST API with a fine-grained token. The token needs, on the
// repository alone: Secrets read and write, Variables read and write, and Metadata read.
type githubREST struct {
	token string
	// owner and name are the repository of the folder, from its git remote.
	owner, name string
}

var githubHTTP = &http.Client{Timeout: 30 * time.Second}

// call sends one request. body is encoded as JSON, out receives a JSON answer, and ok lists the
// statuses that are no error. It returns the status too.
func (g *githubREST) call(ctx context.Context, method, path string, body, out any, ok ...int) (int, error) {
	var in io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		in = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, githubAPIBase+path, in)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+g.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := githubHTTP.Do(req)
	if err != nil {
		var urlErr interface{ Unwrap() error }
		if errors.As(err, &urlErr) {
			err = urlErr.Unwrap()
		}
		return 0, fmt.Errorf("the GitHub API: %s", g.scrub(err.Error()))
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	for _, s := range ok {
		if res.StatusCode == s {
			if out != nil && len(data) > 0 {
				if err := json.Unmarshal(data, out); err != nil {
					return res.StatusCode, fmt.Errorf("%s %s: the answer of GitHub is not what tifa expects: %w", method, path, err)
				}
			}
			return res.StatusCode, nil
		}
	}
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &msg)
	return res.StatusCode, g.statusError(method, path, res.StatusCode, msg.Message)
}

// scrub keeps the token out of a message.
func (g *githubREST) scrub(s string) string { return strings.ReplaceAll(s, g.token, "***") }

func (g *githubREST) statusError(method, path string, status int, message string) error {
	message = g.scrub(strings.TrimSpace(message))
	if message == "" {
		message = "no error text"
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("GitHub refused the token in %s (%d %s): it may be wrong, expired or revoked", githubTokenEnv, status, message)
	case http.StatusForbidden, http.StatusNotFound:
		return fmt.Errorf("GitHub answered %d (%s) to %s %s: the token in %s must be limited to this repository, with the repository permissions Secrets and Variables set to Read and write (Metadata: Read comes with them)",
			status, message, method, path, githubTokenEnv)
	}
	return fmt.Errorf("GitHub answered %d (%s) to %s %s", status, message, method, path)
}

func (g *githubREST) Repo(ctx context.Context) (githubRepo, error) {
	return memoRead(ctx, "rest\x00repo\x00"+g.owner+"/"+g.name, func() (githubRepo, error) {
		var v struct {
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
		}
		if _, err := g.call(ctx, "GET", "/repos/"+g.owner+"/"+g.name, nil, &v, 200); err != nil {
			return githubRepo{}, err
		}
		if !repoName.MatchString(v.FullName) {
			return githubRepo{}, errors.New("GitHub gave no repository name tifa can read")
		}
		if v.DefaultBranch == "" {
			return githubRepo{}, fmt.Errorf("%s has no default branch yet: push the app to it first", v.FullName)
		}
		return githubRepo{Name: v.FullName, Branch: v.DefaultBranch}, nil
	})
}

// pages reads a list page after page until the total is reached.
func pages[T any](ctx context.Context, g *githubREST, path, key string, perPage int, item func(T)) error {
	const maxPages = 100
	seen := 0
	for page := 1; page <= maxPages; page++ {
		var total int
		var items []T
		raw := map[string]json.RawMessage{}
		q := fmt.Sprintf("%s?per_page=%d&page=%d", path, perPage, page)
		if _, err := g.call(ctx, "GET", q, nil, &raw, 200); err != nil {
			return err
		}
		if err := json.Unmarshal(raw["total_count"], &total); err != nil {
			return fmt.Errorf("GET %s: no total_count in the answer", path)
		}
		if err := json.Unmarshal(raw[key], &items); err != nil {
			return fmt.Errorf("GET %s: no %s in the answer", path, key)
		}
		for _, it := range items {
			item(it)
		}
		seen += len(items)
		if len(items) == 0 || seen >= total {
			return nil
		}
	}
	return fmt.Errorf("GET %s lists more than %d pages", path, maxPages)
}

func (g *githubREST) SecretNames(ctx context.Context, repo string) (map[string]bool, error) {
	return memoRead(ctx, "rest\x00secrets\x00"+repo, func() (map[string]bool, error) {
		got := map[string]bool{}
		err := pages(ctx, g, "/repos/"+repo+"/actions/secrets", "secrets", 100, func(s struct {
			Name string `json:"name"`
		}) {
			got[s.Name] = true
		})
		return got, err
	})
}

func (g *githubREST) Variables(ctx context.Context, repo string) (map[string]string, error) {
	return memoRead(ctx, "rest\x00variables\x00"+repo, func() (map[string]string, error) {
		got := map[string]string{}
		err := pages(ctx, g, "/repos/"+repo+"/actions/variables", "variables", 30, func(v struct{ Name, Value string }) { got[v.Name] = v.Value })
		return got, err
	})
}

// SetSecret seals the value with the public key of the repository, a libsodium sealed box, so
// only GitHub can open it. https://docs.github.com/en/rest/guides/encrypting-secrets-for-the-rest-api
func (g *githubREST) SetSecret(ctx context.Context, repo, name, value string) error {
	var key struct {
		ID  string `json:"key_id"`
		Key string `json:"key"`
	}
	if _, err := g.call(ctx, "GET", "/repos/"+repo+"/actions/secrets/public-key", nil, &key, 200); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(key.Key)
	if err != nil || len(raw) != 32 || key.ID == "" {
		return errors.New("GitHub gave a public key tifa cannot read")
	}
	var pub [32]byte
	copy(pub[:], raw)
	sealed, err := box.SealAnonymous(nil, []byte(value), &pub, rand.Reader)
	if err != nil {
		return err
	}
	body := map[string]string{"encrypted_value": base64.StdEncoding.EncodeToString(sealed), "key_id": key.ID}
	_, err = g.call(ctx, "PUT", "/repos/"+repo+"/actions/secrets/"+url.PathEscape(name), body, nil, 201, 204)
	return err
}

// SetVariable replaces the variable, and makes it when it is not there.
func (g *githubREST) SetVariable(ctx context.Context, repo, name, value string) error {
	path := "/repos/" + repo + "/actions/variables"
	status, err := g.call(ctx, "PATCH", path+"/"+url.PathEscape(name), map[string]string{"name": name, "value": value}, nil, 204)
	if status != http.StatusNotFound {
		return err
	}
	_, err = g.call(ctx, "POST", path, map[string]string{"name": name, "value": value}, nil, 201)
	return err
}

func (g *githubREST) DeleteSecret(ctx context.Context, repo, name string) error {
	return g.delete(ctx, "/repos/"+repo+"/actions/secrets/"+url.PathEscape(name))
}

func (g *githubREST) DeleteVariable(ctx context.Context, repo, name string) error {
	return g.delete(ctx, "/repos/"+repo+"/actions/variables/"+url.PathEscape(name))
}

// delete removes one thing. One that is gone already is what the caller wanted.
func (g *githubREST) delete(ctx context.Context, path string) error {
	status, err := g.call(ctx, "DELETE", path, nil, nil, 204)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}
