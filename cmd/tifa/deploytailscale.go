package main

// With deploy.ssh tailscale, the workflow joins the tailnet with an OAuth client of the developer,
// kept as two GitHub secrets. Tifa cannot make the client: only the Tailscale admin console can.
// Setup asks for it on a terminal, checks it can make keys for tag:ci, and hands both values to gh
// on its input. Tifa keeps neither.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// tailscaleAPI is the base of the Tailscale API. Tests point it at a fake.
var tailscaleAPI = "https://api.tailscale.com"

// tailscaleTag is the tag the runner joins the tailnet with, as the workflow writes it.
const tailscaleTag = "tag:ci"

// tailscaleOAuthCommand makes setup ask for the OAuth client again.
const tailscaleOAuthCommand = "tifa deploy setup --tailscale-oauth"

// tailscaleOAuthHow says how to make the client, which only the admin console can.
const tailscaleOAuthHow = "In Tailscale, open https://console.tailscale.com/admin/settings/trust-credentials and make an OAuth client with the auth_keys scope for the tag " + tailscaleTag + ", which your tailnet policy must own and let reach port 22 of the server"

// tailscaleSteps plans the OAuth client CI joins the tailnet with. GitHub does not show a secret,
// so two that are there are taken as right, unless --tailscale-oauth asks for them again.
func (r *setupRun) tailscaleSteps(p *setupPlan) {
	have := r.ghNames[secretTSID] && r.ghNames[secretTSSecret]
	switch {
	case have && !r.tailscaleOAuth:
		p.ok("github: the Tailscale secrets " + secretTSID + " and " + secretTSSecret + ". To replace them: " + tailscaleOAuthCommand)
	case !stdinIsTerminal(r.in):
		p.cannot("github: the Tailscale secrets "+secretTSID+" and "+secretTSSecret,
			tailscaleOAuthHow+". Then run "+tailscaleOAuthCommand+" on a terminal to paste it")
	default:
		op := opAdd
		if have {
			op = opChange
		}
		text := "github: the Tailscale OAuth client, pasted by you and kept as the secrets " + secretTSID + " and " + secretTSSecret
		p.steps = append(p.steps, planStep{op: op, text: text, soft: true, do: r.pasteTailscaleOAuth})
	}
}

// pasteTailscaleOAuth says how to make the client, reads its ID and secret hidden, checks them and sets the secrets. A client that fails the check is asked for again. An empty ID
// skips, which leaves CI unable to reach the server, so it is a warning.
func (r *setupRun) pasteTailscaleOAuth(ctx context.Context) (string, error) {
	say := func(f string, a ...any) { fmt.Fprintf(r.stdout, "          "+f+"\n", a...) }
	ask := secretAsk{
		Title: "Tailscale OAuth client",
		For:   r.t.app + " · " + r.repo.Name,
		Why: "GitHub Actions joins your tailnet with this client to reach the server, and Tifa keeps it as the GitHub secrets " +
			secretTSID + " and " + secretTSSecret + ". Make it under Trust credentials with the auth_keys scope for the tag " + tailscaleTag +
			", which your tailnet policy must own and let reach port 22 of the server.",
		Link:     "https://console.tailscale.com/admin/settings/trust-credentials",
		LinkText: "Open Trust credentials in Tailscale",
		Fields:   []string{"Client ID", "Client secret"},
		Skip:     "Leave them empty to skip: CI cannot reach the server until you add them.",
	}
	terminal := stdinIsTerminal(r.in)
	if terminal {
		say("%s. Paste its client ID and secret here, or press Enter to skip:", tailscaleOAuthHow)
	}
	for try := 1; try <= purgeTokenTries; try++ {
		values, err := askSecrets(ctx, r.in, r.errOut, ask)
		if err != nil {
			return "", fmt.Errorf("reading the client: %w. To try again: %s", err, tailscaleOAuthCommand)
		}
		id, secret := strings.TrimSpace(values[0]), strings.TrimSpace(values[1])
		if id == "" {
			return "", fmt.Errorf("skipped, and CI cannot reach the server without it. To add it: %s", tailscaleOAuthCommand)
		}
		err = checkTailscaleOAuth(ctx, id, secret)
		if err == nil {
			if err := r.ghSecret(ctx, secretTSID, id); err != nil {
				return "", err
			}
			return "", r.ghSecret(ctx, secretTSSecret, secret)
		}
		if try == purgeTokenTries {
			return "", fmt.Errorf("the client was refused: %v. To try again: %s", err, tailscaleOAuthCommand)
		}
		ask.Problem = fmt.Sprintf("The client was refused: %v. Paste it again, or leave it empty to skip.", err)
		if terminal {
			say("the client was refused: %v. Paste it again, or press Enter to skip.", err)
		}
	}
	return "", nil
}

// checkTailscaleOAuth asks the Tailscale API for a token with the scope auth_keys and the tag of
// the runner, which is what the GitHub action does. It makes no key and no device. The secret is in
// the request body only, and in no error.
func checkTailscaleOAuth(ctx context.Context, id, secret string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	form := url.Values{"client_id": {id}, "client_secret": {secret}, "scope": {"auth_keys"}, "tags": {tailscaleTag}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tailscaleAPI+"/api/v2/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		var urlErr interface{ Unwrap() error }
		if errors.As(err, &urlErr) {
			err = urlErr.Unwrap()
		}
		return fmt.Errorf("the Tailscale API: %w", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusOK {
		return nil
	}
	var out struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &out)
	if out.Message == "" {
		out.Message = "no error text"
	}
	return fmt.Errorf("the Tailscale API answered %d: %s", res.StatusCode, out.Message)
}
