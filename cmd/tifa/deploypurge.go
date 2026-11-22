package main

// What tifa deploy and tifa deploy rollback do after a release is live: clear the CDN cache of
// the domain of the app, and write the result into the summary of a GitHub Actions run.
//
// The cache is cleared by host name, never the whole zone, with the first of these that is there:
//
//  1. TIFA_DEPLOY_CF_PURGE_TOKEN, a Cloudflare API token that can only purge the cache of one
//     zone, and TIFA_DEPLOY_CF_ZONE_ID, the ID of that zone: inputs of tifa deploy, from the
//     environment, else the .env of the app. They override the server, and an older workflow
//     still hands them from secrets of the repository.
//  2. The file of purge tokens on the server, which setup writes: a token and the zone ID for
//     each zone, readable only by the deploy user. Deploy reads it over its own ssh, so a deploy
//     from CI and from a machine without cf clear the cache alike.
//  3. cf, logged in on this machine.
//
// The token lives in memory only: it goes on the input of ssh and in the Authorization header,
// never in a command line, a log or an error.

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
	envPurgeToken = "TIFA_DEPLOY_CF_PURGE_TOKEN"
	envZoneID     = "TIFA_DEPLOY_CF_ZONE_ID"
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
	// NoToken is set when there is neither a token nor a cf login. Then the cache is left to run
	// out, which the developer chose, so it is no warning.
	NoToken bool
}

func (r cacheResult) String() string {
	switch {
	case r.Host == "":
		return "not used, deploy.front is not cloudflare"
	case r.Cleared:
		return "cleared for " + r.Host
	case r.NoToken:
		return "not cleared for " + r.Host + ": " + noTokenCost + ". " + noTokenHow()
	}
	return "NOT cleared for " + r.Host + ": " + r.Warning
}

// clearCache clears the CDN cache of the domain of t, which is empty when the app has no domain.
// It runs once the release is live, and goes by the order at the top of this file.
func clearCache(ctx context.Context, t deployTarget, stderr io.Writer) cacheResult {
	domain := t.domain
	r := cacheResult{Host: domain}
	if domain == "" {
		return r
	}
	token, _, err := readDeployInput(t.root, "", "", envPurgeToken)
	if err != nil {
		r.Warning = err.Error()
		return r
	}
	var zoneID string
	if token != "" {
		if zoneID, _, err = readDeployInput(t.root, "", "", envZoneID); err == nil && zoneID == "" {
			err = fmt.Errorf("%s is set and %s is not: put the ID of the zone in the environment or in the .env at the root of the app (the token only clears the cache, so it cannot read the zone to find it)", envPurgeToken, envZoneID)
		}
	} else {
		var c purgeCred
		var ok bool
		if c, ok, err = t.serverPurge(ctx, stderr); ok {
			token, zoneID = c.Token, c.ZoneID
		}
	}
	switch {
	case err != nil:
	case token != "":
		err = purgeWithToken(ctx, token, zoneID, domain)
	case cfLoggedIn(ctx):
		var zone cfZone
		if zone, err = findZone(ctx, domain); err == nil {
			_, err = runCF(ctx, "cache", "purge", "-z", zone.Name, "--force", "--body", string(purgeBody(domain)))
		}
	default:
		r.NoToken = true
		return r
	}
	if err != nil {
		r.Warning = err.Error()
		return r
	}
	r.Cleared = true
	return r
}

// purgeDir and purgeFile are where setup keeps the purge tokens on the server, under the home of
// the deploy user: a folder only it may enter, and a file only it may read. One line per zone: the
// name of the zone, its ID and the token, separated by tabs. The apps of the server do not read it.
const (
	purgeDir  = ".config/tifa"
	purgeFile = purgeDir + "/cloudflare-purge"
)

// purgeFileHeader is the first line of the file.
const purgeFileHeader = "# Written by tifa deploy setup. A Cloudflare token that can only clear the CDN cache, for each zone: name, ID, token."

// purgeCred is the line of a zone in the file.
type purgeCred struct{ Zone, ZoneID, Token string }

// purgeStoreBody runs as the deploy user with the line of a zone on its input. It writes the file
// again with that line in place of the old line of the zone, and the other lines as they were.
// The token passes through cat and the shell only: it is on no command line.
const purgeStoreBody = `set -eu
line=$(cat)
zone=${line%%"$tab"*}
umask 022
mkdir -p "$HOME/.config"
umask 077
d="$HOME/` + purgeDir + `"
f="$HOME/` + purgeFile + `"
mkdir -p "$d"
chmod 700 "$d"
tmp=$(mktemp "$f.XXXXXX")
trap 'rm -f "$tmp"' EXIT
printf '%s\n' "$header" > "$tmp"
if [ -f "$f" ]; then
	while IFS= read -r l || [ -n "$l" ]; do
		case "$l" in ''|'#'*|"$zone$tab"*) continue ;; esac
		printf '%s\n' "$l" >> "$tmp"
	done < "$f"
fi
printf '%s\n' "$line" >> "$tmp"
chmod 600 "$tmp"
mv "$tmp" "$f"
trap - EXIT
`

// purgeReadBody runs as the deploy user and prints the file, nothing when there is none.
const purgeReadBody = `set -eu
f="$HOME/` + purgeFile + `"
if [ -f "$f" ]; then cat "$f"; fi
`

// purgeForgetBody runs as the deploy user once no app is left on the server, and deletes the file
// and its folder.
const purgeForgetBody = `set -eu
rm -f "$HOME/` + purgeFile + `"
rmdir "$HOME/` + purgeDir + `" 2>/dev/null || true
`

// purgeField says whether s can be a field of the file: not empty, and no space, tab, line end or
// other control character.
func purgeField(s string) bool {
	return s != "" && !strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f })
}

// storePurge keeps c on the server, in place of what the zone had.
func (t deployTarget) storePurge(ctx context.Context, c purgeCred, stderr io.Writer) error {
	if !purgeField(c.Zone) || !purgeField(c.ZoneID) || !purgeField(c.Token) {
		return errors.New("the zone, its ID and the token must each be one word, with no space or line end")
	}
	body := script(purgeStoreBody, "tab", "\t", "header", purgeFileHeader)
	line := strings.NewReader(c.Zone + "\t" + c.ZoneID + "\t" + c.Token + "\n")
	if err := t.ssh(ctx, deployUser, body, line, io.Discard, stderr); err != nil {
		return fmt.Errorf("keeping the token in ~/%s on the server: %w", purgeFile, err)
	}
	return nil
}

// serverPurge reads the file on the server and returns the line of the zone of the domain of t.
// ok is false when the file has none.
func (t deployTarget) serverPurge(ctx context.Context, stderr io.Writer) (c purgeCred, ok bool, err error) {
	var out bytes.Buffer
	if err := t.ssh(ctx, deployUser, purgeReadBody, nil, &out, stderr); err != nil {
		return c, false, fmt.Errorf("reading ~/%s on the server: %w", purgeFile, err)
	}
	return pickPurgeLine(out.String(), t.domain)
}

// pickPurgeLine finds the line of the zone of domain in the text of the file: the zone that is the
// domain, or else the longest one the domain ends in. An error never holds a field of a line.
func pickPurgeLine(text, domain string) (c purgeCred, ok bool, err error) {
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 || !purgeField(f[0]) || !purgeField(f[1]) || !purgeField(f[2]) {
			return purgeCred{}, false, fmt.Errorf("line %d of ~/%s on the server is not a zone, its ID and a token: run go tool tifa deploy setup --cache-purge-token", i+1, purgeFile)
		}
		if (domain == f[0] || strings.HasSuffix(domain, "."+f[0])) && len(f[0]) > len(c.Zone) {
			c, ok = purgeCred{Zone: f[0], ZoneID: f[1], Token: f[2]}, true
		}
	}
	return c, ok, nil
}

// printCache prints the result of clearCache, a warning when it failed.
func printCache(w io.Writer, r cacheResult) {
	switch {
	case r.Host == "":
	case r.Cleared:
		fmt.Fprintf(w, "cache: cleared for %s\n", r.Host)
	case r.NoToken:
		fmt.Fprintf(w, "cache: not cleared for %s: %s. %s\n", r.Host, noTokenCost, noTokenHow())
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
	if s.Cache.Host != "" && !s.Cache.Cleared && !s.Cache.NoToken && s.Err == nil {
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

// The purge token is an account API token that can only clear the cache of one zone. The developer
// makes it in the Cloudflare dashboard, because a login of cf cannot make API tokens. Tifa gets it
// pasted, checks it and keeps it on the server, and nowhere else.

// purgeTokenName is the name the form of the token suggests, after the zone it may clear.
func purgeTokenName(zone string) string { return "tifa cache purge " + zone }

// repoPurgeTokenName is the name an earlier setup suggested, for the token it kept as a secret of
// the repository.
func repoPurgeTokenName(repo string) string { return "tifa cache purge " + repo }

// purgeTokenLink is the Cloudflare form of an account API token with the permission Cache Purge,
// filled in. https://developers.cloudflare.com/fundamentals/api/how-to/account-owned-token-template/
func purgeTokenLink(zone string) string {
	esc := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	return "https://dash.cloudflare.com/?to=/:account/api-tokens&permissionGroupKeys=" +
		esc(`[{"key":"cache","type":"purge"}]`) + "&name=" + esc(purgeTokenName(zone))
}

// checkPurgeToken clears a file that does not exist, which proves the token may purge the zone
// without touching a cached page.
func checkPurgeToken(ctx context.Context, token, zoneID, domain string) error {
	body, _ := json.Marshal(map[string][]string{"files": {"https://" + domain + "/.well-known/tifa-purge-check"}})
	return purgeRequest(ctx, token, zoneID, body)
}

// noTokenCost is what a deploy that does not clear the CDN cache costs. A CDN keeps its copies
// until they run out: that is the price of a CDN, not an error.
const noTokenCost = "a page with Cache stays on the CDN until its Cache time runs out. That copy may ask for scripts the new build no longer has"

// noTokenHow says how to have the cache cleared at each deploy.
func noTokenHow() string {
	return "To clear it at each deploy, run go tool tifa deploy setup --cache-purge-token once: it keeps a token on the server that can only clear this cache"
}
