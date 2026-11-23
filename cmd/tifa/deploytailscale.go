package main

// deploy.ssh tailscale: the runner of GitHub Actions joins the tailnet, and only it does. Your own
// machine reaches the server however it already does, with the address in TIFA_DEPLOY_HOST.
//
// CI joins with Tailscale workload identity federation: the runner gets a token from GitHub that
// says which repository it is, and Tailscale trades it for a key that lets the runner join with the
// tag tag:ci. Nothing in the repository is secret: the client ID and the audience of the identity
// are kept as two repository variables. Only the Tailscale admin console can make the identity, so
// setup says how and takes its client ID from the flag --tailscale-client-id or, on a terminal,
// from a question.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// tailscaleTag is the tag the runner joins the tailnet with, as the workflow writes it.
const tailscaleTag = "tag:ci"

// tailscaleConsole is the page of the admin console that makes the identity.
const tailscaleConsole = "https://console.tailscale.com/admin/settings/trust-credentials"

// githubIssuer is the issuer the identity trusts: the one GitHub signs the tokens of its runners with.
const githubIssuer = "https://token.actions.githubusercontent.com"

// tailscaleClientCommand makes setup take the client ID of the identity.
const tailscaleClientCommand = "go tool tifa deploy setup --tailscale-client-id <client ID>"

// tailscaleClientID is what a client ID looks like.
var tailscaleClientID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// checkTailscaleClientID refuses a value that cannot be the client ID of an identity. A key of
// Tailscale (tskey-...) is a secret: it is never taken, and never shown.
func checkTailscaleClientID(id string) error {
	switch {
	case strings.HasPrefix(id, "tskey-"):
		return fmt.Errorf("that is a Tailscale key, which is a secret and which tifa does not take. The client ID of a federated identity is not secret: copy it from the page of the credential")
	case !tailscaleClientID.MatchString(id):
		return fmt.Errorf("%q is not a client ID: it is letters, digits, dots, hyphens and underscores", id)
	}
	return nil
}

// tailscaleAudience is the audience of an identity, as Tailscale makes it from the client ID.
func tailscaleAudience(id string) string { return "api.tailscale.com/" + id }

// tailscaleIdentityHow says how to make the identity, which only the admin console can do.
func (r *setupRun) tailscaleIdentityHow() string {
	return fmt.Sprintf("In Tailscale, open %s, choose Credential, then OpenID Connect. Pick the GitHub issuer (%s), set the subject to repo:%s:ref:refs/heads/%s (GitHub gives a run started by the CI workflow, and a manual run on the default branch, that ref), "+
		"give it the scope auth_keys for the tag %s, and generate it. Copy its client ID: neither it nor the audience is a secret. "+
		"Your tailnet policy must have %s in tagOwners and let it reach the server on its ssh port",
		tailscaleConsole, githubIssuer, r.repo.Name, r.repo.Branch, tailscaleTag, tailscaleTag)
}

// tailscaleIdentitySteps plans the identity CI joins the tailnet with: the two repository
// variables. They are not secret, so the plan can read what GitHub has, and a client ID given with
// the flag that differs from it is a change.
func (r *setupRun) tailscaleIdentitySteps(p *setupPlan) {
	have := r.ghVars[varTSClient] != "" && r.ghVars[varTSAudience] != ""
	given := strings.TrimSpace(r.tailscaleClientID)
	legacy := r.ghNames[secretTSID] && r.ghNames[secretTSSecret]
	text := "github: the Tailscale identity CI joins your tailnet with, the variables " + varTSClient + " and " + varTSAudience
	switch {
	case have && (given == "" || given == r.ghVars[varTSClient]):
		p.ok("github: the Tailscale identity, the variables " + varTSClient + " and " + varTSAudience)
		return
	case given == "" && !stdinIsTerminal(r.in) && legacy:
		// CI keeps joining with the OAuth client of an older setup, which the workflow still hands over.
		p.notes = append(p.notes, planNote{what: text, why: "CI still joins with the OAuth secrets " + secretTSID + " and " + secretTSSecret + ", which hold a long-lived secret. " +
			r.tailscaleIdentityHow() + ". Then run " + tailscaleClientCommand + ", and setup writes the workflow for it and removes the two secrets"})
		return
	case given == "" && !stdinIsTerminal(r.in):
		p.cannot(text, r.tailscaleIdentityHow()+". Then run "+tailscaleClientCommand+": the client ID is not secret, so it may be given on a command line")
		return
	}
	op := opAdd
	if have {
		op = opChange
	}
	r.tailscalePlanned = true
	p.steps = append(p.steps, planStep{op: op, text: text, soft: true, do: r.setTailscaleIdentity})
}

// setTailscaleIdentity takes the client ID, from the flag or from a question on the terminal, and
// sets the variables. An empty answer skips.
func (r *setupRun) setTailscaleIdentity(ctx context.Context) (string, error) {
	id := strings.TrimSpace(r.tailscaleClientID)
	if id == "" {
		fmt.Fprintf(r.stdout, "          %s.\n          Paste its client ID, or press Enter to skip: ", r.tailscaleIdentityHow())
		id = strings.TrimSpace(readLine(r.in))
		if id == "" {
			fmt.Fprintln(r.stdout)
			return "", fmt.Errorf("skipped, and CI cannot join the tailnet without it. To add it: %s", tailscaleClientCommand)
		}
	}
	if err := checkTailscaleClientID(id); err != nil {
		return "", err
	}
	if err := r.gh.SetVariable(ctx, r.repo.Name, varTSClient, id); err != nil {
		return "", err
	}
	if err := r.gh.SetVariable(ctx, r.repo.Name, varTSAudience, tailscaleAudience(id)); err != nil {
		return "", err
	}
	r.tailscaleSet = true
	return "", nil
}

// legacyTailscale says the workflow keeps joining with the OAuth client of an older setup: the
// repository has its two secrets and no identity yet, and none was set by this run.
func (r *setupRun) legacyTailscale(set bool) bool {
	haveIdentity := r.ghVars[varTSClient] != "" && r.ghVars[varTSAudience] != ""
	return r.ghNames[secretTSID] && r.ghNames[secretTSSecret] && !haveIdentity && !set
}

// oldTailscaleSteps takes away the OAuth secrets of an older setup once the workflow on the default
// branch no longer hands them to the Tailscale action, and the identity variables when deploy.ssh
// is not tailscale any more. CI runs the workflow of the default branch, so the secrets stay until
// that file stops using them.
func (r *setupRun) oldTailscaleSteps(ctx context.Context, p *setupPlan) {
	if r.ghVars[varMarker] == "" {
		return
	}
	t := r.t
	repo, branch := r.repo.Name, r.repo.Branch
	var vars []string
	if t.sshMode != connTailscale {
		for _, n := range []string{varTSClient, varTSAudience} {
			if r.ghVars[n] != "" {
				vars = append(vars, n)
			}
		}
	}
	var secrets []string
	for _, n := range []string{secretTSID, secretTSSecret} {
		if r.ghNames[n] {
			secrets = append(secrets, n)
		}
	}
	if len(vars) > 0 {
		p.add(opRemove, "github: the variables "+strings.Join(vars, " and ")+", which tifa set and deploy.ssh no longer needs", func(ctx context.Context) (string, error) {
			for _, n := range vars {
				if err := r.gh.DeleteVariable(ctx, repo, n); err != nil {
					return "", err
				}
			}
			return "", nil
		})
	}
	if len(secrets) == 0 {
		return
	}
	these := "the secrets " + strings.Join(secrets, " and ")
	if len(secrets) == 1 {
		these = "the secret " + secrets[0]
	}
	if t.sshMode == connTailscale && !r.tailscalePlanned && (r.ghVars[varTSClient] == "" || r.ghVars[varTSAudience] == "") {
		return // CI still joins with them: the note of the identity says what to do
	}
	onMain, found, err := r.gh.FileOnBranch(ctx, repo, branch, githubWorkflowPath)
	switch {
	case err != nil:
		p.notes = append(p.notes, planNote{what: "github: " + these + " stay", why: fmt.Sprintf("Setup cannot read %s on %s, so it cannot tell whether CI still uses them: %v. Once the workflow on %s no longer has them, run setup again, or delete them by hand", githubWorkflowPath, branch, err, branch)})
		return
	case found && (strings.Contains(onMain, "secrets."+secretTSID) || strings.Contains(onMain, "secrets."+secretTSSecret)):
		p.notes = append(p.notes, planNote{what: fmt.Sprintf("github: %s stay while the workflow on %s hands them to the Tailscale action", these, branch), why: fmt.Sprintf("Commit and push %s, which no longer does, then run setup again to remove them", githubWorkflowPath)})
		return
	}
	p.add(opRemove, "github: "+these+", the OAuth client of an older setup, which the workflow no longer uses. Delete the OAuth client itself in Tailscale, Trust credentials, when nothing else uses it", func(ctx context.Context) (string, error) {
		if t.sshMode == connTailscale && !r.tailscaleSet && r.ghVars[varTSClient] == "" {
			return "", skipped{"no identity is set yet, so the secrets stay until one is"}
		}
		for _, n := range secrets {
			if err := r.gh.DeleteSecret(ctx, repo, n); err != nil {
				return "", err
			}
		}
		return "", nil
	})
}
