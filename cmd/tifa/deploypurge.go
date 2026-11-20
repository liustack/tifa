package main

// What tifa deploy and tifa deploy rollback do after a release is live: clear the CDN cache of
// deploy.domain, and write the result into the summary of a GitHub Actions run.
//
// The cache is cleared by host name, never the whole zone. On a machine with cf logged in it is
// cf that does it. In CI there is no cf: CF_PURGE_TOKEN, a Cloudflare API token that can only
// purge the cache of one zone, and CF_ZONE_ID, the ID of that zone (not a secret, the workflow
// carries it), go to the Cloudflare API directly. Tifa keeps neither.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Names of the environment variables CI sets for the purge.
const (
	envPurgeToken = "CF_PURGE_TOKEN"
	envZoneID     = "CF_ZONE_ID"
	// envStepSummary is the file GitHub Actions gives a step to write a Markdown summary into.
	envStepSummary = "GITHUB_STEP_SUMMARY"
)

// cloudflareAPI is the base of the Cloudflare API. Tests point it at a fake.
var cloudflareAPI = "https://api.cloudflare.com/client/v4"

// purgeBody is the request that clears the cache of the host names and nothing else.
func purgeBody(hosts ...string) []byte {
	b, _ := json.Marshal(map[string][]string{"hosts": hosts})
	return b
}

// purgeWithToken asks the Cloudflare API to clear the cache of host in the zone. The token is in
// the Authorization header only, and in no error.
func purgeWithToken(ctx context.Context, token, zoneID, host string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudflareAPI+"/zones/"+zoneID+"/purge_cache", bytes.NewReader(purgeBody(host)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		var urlErr interface{ Unwrap() error }
		if errors.As(err, &urlErr) {
			err = urlErr.Unwrap()
		}
		return fmt.Errorf("the Cloudflare API: %w", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out struct {
		Success bool `json:"success"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(data, &out)
	if res.StatusCode == http.StatusOK && out.Success {
		return nil
	}
	var msgs []string
	for _, e := range out.Errors {
		msgs = append(msgs, fmt.Sprintf("[%d] %s", e.Code, e.Message))
	}
	if len(msgs) == 0 {
		msgs = append(msgs, "no error text")
	}
	return fmt.Errorf("the Cloudflare API answered %d: %s", res.StatusCode, strings.Join(msgs, "; "))
}

// cacheResult is what clearing the CDN cache came to.
type cacheResult struct {
	Host    string
	Cleared bool
	// Warning says why the cache was not cleared. A deploy does not fail for it.
	Warning string
}

func (r cacheResult) String() string {
	switch {
	case r.Host == "":
		return "not used, deploy.domain is not set"
	case r.Cleared:
		return "cleared for " + r.Host
	}
	return "NOT cleared for " + r.Host + ": " + r.Warning
}

// clearCache clears the CDN cache of domain, which is empty when the app has no domain.
func clearCache(ctx context.Context, domain string) cacheResult {
	r := cacheResult{Host: domain}
	if domain == "" {
		return r
	}
	var err error
	switch token := os.Getenv(envPurgeToken); {
	case token != "":
		zone := os.Getenv(envZoneID)
		if zone == "" {
			err = fmt.Errorf("%s is set and %s is not, and the workflow written by tifa deploy setup sets both", envPurgeToken, envZoneID)
			break
		}
		err = purgeWithToken(ctx, token, zone, domain)
	case cfLoggedIn(ctx):
		var zone cfZone
		if zone, err = findZone(ctx, domain); err == nil {
			_, err = runCF(ctx, "cache", "purge", "-z", zone.Name, "--force", "--body", string(purgeBody(domain)))
		}
	default:
		err = fmt.Errorf("cf is not logged in and %s is not set: clear it in Cloudflare, Caching, Configuration, Custom Purge, by host name", envPurgeToken)
	}
	if err != nil {
		r.Warning = err.Error()
		return r
	}
	r.Cleared = true
	return r
}

// printCache prints the result of clearCache, a warning when it failed.
func printCache(w io.Writer, r cacheResult) {
	switch {
	case r.Host == "":
	case r.Cleared:
		fmt.Fprintf(w, "cache: cleared for %s\n", r.Host)
	default:
		fmt.Fprintf(w, "warning: the CDN cache of %s was not cleared: %s\n", r.Host, r.Warning)
	}
}

// deploySummary is what a deploy or rollback tells the summary of a GitHub Actions run.
type deploySummary struct {
	App, Action, Release, Commit string
	Elapsed                      time.Duration
	Health                       string // the health check line of the server
	Cache                        cacheResult
	Err                          error
}

// markdown is the summary: a heading and a table, short enough to read on the run page.
func (s deploySummary) markdown() string {
	var b strings.Builder
	result := "done"
	if s.Err != nil {
		result = "failed"
	}
	verb := map[string]string{"deploy": "Deploy", "rollback": "Rollback"}[s.Action]
	fmt.Fprintf(&b, "### %s of %s %s\n\n", verb, s.App, result)
	b.WriteString("| | |\n| --- | --- |\n")
	row := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "| %s | %s |\n", k, strings.ReplaceAll(v, "|", `\|`))
		}
	}
	row("App", s.App)
	row("Release", s.Release)
	row("Commit", s.Commit)
	row("Time", s.Elapsed.Round(100*time.Millisecond).String())
	row("Health check", s.Health)
	if s.Err == nil {
		row("CDN cache", s.Cache.String())
	} else {
		row("Error", strings.ReplaceAll(s.Err.Error(), "\n", " "))
	}
	if s.Cache.Host != "" && !s.Cache.Cleared && s.Err == nil {
		fmt.Fprintf(&b, "\n> Warning: the CDN cache of %s was not cleared. The deploy itself went through.\n", s.Cache.Host)
	}
	b.WriteString("\n")
	return b.String()
}

// writeStepSummary appends s to the file GITHUB_STEP_SUMMARY names, when the variable is set. This
// is the file contract of GitHub Actions, not a guess about who runs tifa.
func writeStepSummary(s deploySummary) error {
	path := os.Getenv(envStepSummary)
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(s.markdown())
	return errors.Join(err, f.Close())
}

// healthCapture passes what the server prints on and remembers its health check line.
type healthCapture struct {
	w    io.Writer
	line string
	buf  []byte
}

func (h *healthCapture) Write(p []byte) (int, error) {
	h.buf = append(h.buf, p...)
	for {
		i := bytes.IndexByte(h.buf, '\n')
		if i < 0 {
			break
		}
		if l := string(h.buf[:i]); strings.HasPrefix(l, "health check: ") {
			h.line = strings.TrimPrefix(l, "health check: ")
		}
		h.buf = h.buf[i+1:]
	}
	return h.w.Write(p)
}

// purge token of a repository: a user API token that can only clear the cache of one zone.

// purgeTokenName names the token of a repository for a zone. The prefix with the repository finds
// the tokens to remove when the zone changes or the repository leaves.
func purgeTokenName(repo, zone string) string { return purgeTokenPrefix(repo) + zone }

func purgeTokenPrefix(repo string) string {
	return "tifa-purge-" + strings.ReplaceAll(repo, "/", "-") + "-"
}

type cfToken struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// purgeTokens lists the tokens of the repository, whatever zone they are for.
func purgeTokens(ctx context.Context, repo string) ([]cfToken, error) {
	all, err := cfPages[cfToken](ctx, "user", "tokens", "list")
	if err != nil {
		return nil, err
	}
	var out []cfToken
	for _, t := range all {
		if strings.HasPrefix(t.Name, purgeTokenPrefix(repo)) {
			out = append(out, t)
		}
	}
	return out, nil
}

// createPurgeToken makes the token called name, with the permission Cache Purge on the zone and
// nothing else, and returns its value, which Cloudflare shows only now.
func createPurgeToken(ctx context.Context, name string, zone cfZone) (string, error) {
	var groups []struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := cfJSON(ctx, &groups, "user", "tokens", "permission-groups", "list", "--name", "Cache Purge"); err != nil {
		return "", err
	}
	id := ""
	for _, g := range groups {
		zoneScoped := len(g.Scopes) == 0
		for _, sc := range g.Scopes {
			zoneScoped = zoneScoped || strings.HasSuffix(sc, ".zone")
		}
		if g.Name == "Cache Purge" && zoneScoped {
			id = g.ID
		}
	}
	if id == "" {
		return "", errors.New(`Cloudflare lists no permission group "Cache Purge" for a zone`)
	}
	policies, _ := json.Marshal([]map[string]any{{
		"effect":            "allow",
		"resources":         map[string]string{"com.cloudflare.api.account.zone." + zone.ID: "*"},
		"permission_groups": []map[string]string{{"id": id}},
	}})
	var made struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	}
	if err := cfJSON(ctx, &made, "user", "tokens", "create", "--name", name, "--policies", string(policies)); err != nil {
		return "", err
	}
	if made.Value == "" {
		return "", errors.New("cf gave no token value tifa can read")
	}
	return made.Value, nil
}

// purgeTokenManual says how to make the token by hand and give it to the repository.
func purgeTokenManual(zone, domain string) string {
	return fmt.Sprintf("make it in the Cloudflare dashboard (My Profile, API Tokens, Create Token, Custom token) with the one permission Zone, Cache Purge, Purge, on the zone %s only, then run gh secret set %s, which asks for the value. Without it a deploy still works and prints a warning that the cache of %s was not cleared", zone, envPurgeToken, domain)
}
