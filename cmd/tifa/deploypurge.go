package main

// What tifa deploy and tifa deploy rollback do after a release is live: clear the CDN cache of
// the domain of the app, and write the result into the summary of a GitHub Actions run.
//
// The cache is cleared by host name, never the whole zone. On a machine with cf logged in it is
// cf that does it. In CI there is no cf: CF_CACHE_PURGE_TOKEN, a Cloudflare API token that can only
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
	"net/url"
	"os"
	"strings"
	"time"
)

// Names of the environment variables CI sets for the purge.
const (
	envPurgeToken = "CF_CACHE_PURGE_TOKEN"
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
	return purgeRequest(ctx, token, zoneID, purgeBody(host))
}

// purgeRequest sends body to the purge endpoint of the zone with the token.
func purgeRequest(ctx context.Context, token, zoneID string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudflareAPI+"/zones/"+zoneID+"/purge_cache", bytes.NewReader(body))
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
		return "not used, deploy.front is not cloudflare"
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

// The purge token of a repository is an account API token that can only clear the cache of one
// zone. The developer makes it in the Cloudflare dashboard, because a login of cf cannot make API
// tokens. Tifa gets it pasted, checks it and hands it to gh, and keeps it nowhere else.

// purgeTokenName is the name the form of the token suggests.
func purgeTokenName(repo string) string { return "tifa cache purge " + repo }

// purgeTokenLink is the Cloudflare form of an account API token with the permission Cache Purge,
// filled in. https://developers.cloudflare.com/fundamentals/api/how-to/account-owned-token-template/
func purgeTokenLink(repo string) string {
	esc := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	return "https://dash.cloudflare.com/?to=/:account/api-tokens&permissionGroupKeys=" +
		esc(`[{"key":"cache","type":"purge"}]`) + "&name=" + esc(purgeTokenName(repo))
}

// checkPurgeToken clears a file that does not exist, which proves the token may purge the zone
// without touching a cached page.
func checkPurgeToken(ctx context.Context, token, zoneID, domain string) error {
	body, _ := json.Marshal(map[string][]string{"files": {"https://" + domain + "/.well-known/tifa-purge-check"}})
	return purgeRequest(ctx, token, zoneID, body)
}

// purgeTokenManual says how to make the token by hand and give it to the repository.
func purgeTokenManual(repo, domain string) string {
	return fmt.Sprintf("open %s, set Zone Resources to the zone of %s only, create the token, then run gh secret set %s --repo %s, which asks for the value. Without it a deploy still works and prints a warning that the cache of %s was not cleared",
		purgeTokenLink(repo), domain, envPurgeToken, repo, domain)
}
