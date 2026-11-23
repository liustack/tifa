package main

// deploy.connect tailscale: the runner of GitHub Actions joins the tailnet, and only it does. Your own
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
		// The value is not printed: what was refused may be a secret that is not a Tailscale key.
		return fmt.Errorf("that is not a client ID: it is letters, digits, dots, hyphens and underscores")
	}
	return nil
}

// tailscaleAudience is the audience of an identity, as Tailscale makes it from the client ID.
func tailscaleAudience(id string) string { return "api.tailscale.com/" + id }

// tailscaleRef is the branch the runner has as its ref when a deploy starts by itself, which is the
// branch in the subject of the identity. A run of workflow_run has the default branch as its ref,
// whichever branch was checked. A run of push, which is what deploy.auto.needs: none makes, has
// the branch that was pushed.
func (r *setupRun) tailscaleRef() string {
	if r.t.auto.Needs == "none" {
		return r.branch
	}
	return r.repo.Branch
}

// tailscaleIdentityHow says how to make the identity, which only the admin console can do. GitHub
// has two forms of the subject, and no field says for sure which one a repository gets: the
// immutable form (with the IDs of the owner and the repository) is the default for repositories made
// after July 15, 2026, and for renames and transfers after it, and older ones opt in. So both forms
// are given, with how to see which one GitHub sends.
func (r *setupRun) tailscaleIdentityHow() string {
	owner, name, _ := strings.Cut(r.repo.Name, "/")
	ref := "ref:refs/heads/" + r.tailscaleRef()
	return fmt.Sprintf("In Tailscale, open %s, choose Credential, then OpenID Connect. Pick the GitHub issuer (%s), set the subject to repo:%s:%s (%s, and a manual run on that branch). "+
		"That is the form GitHub sends for a repository made before July 15, 2026. A repository made after that date, one that opted in to immutable subject claims, and one renamed or transferred after the date send repo:%s@<owner ID>/%s@<repository ID>:%s instead: "+
		"get the IDs with gh api repos/%s --jq '.owner.id, .id', and see which form GitHub sends by running github/actions-oidc-debugger in a workflow, or by reading use_immutable_subject at GET /repos/%s/actions/oidc/customization/sub (it tells whether the repository opted in). "+
		"Give the credential the scope auth_keys for the tag %s, and generate it. Copy its client ID: neither it nor the audience is a secret. "+
		"Your tailnet policy must have %s in tagOwners and let it reach the server on its ssh port",
		tailscaleConsole, githubIssuer, r.repo.Name, ref, r.tailscaleRunKind(), owner, name, ref, r.repo.Name, r.repo.Name, tailscaleTag, tailscaleTag)
}

// tailscaleRunKind says why the subject has the ref of tailscaleRef.
func (r *setupRun) tailscaleRunKind() string {
	if r.t.auto.Needs == "none" {
		return "GitHub gives a run started by a push to " + r.branch + " that ref"
	}
	return "GitHub gives the run that starts after " + r.t.auto.Needs + " passed (workflow_run) the default branch as its ref, whatever branch was checked"
}

// tailscaleIdentitySteps plans the identity CI joins the tailnet with: the two repository
// variables. They are not secret, so the plan can read what GitHub has, and a client ID given with
// the flag that differs from it is a change.
func (r *setupRun) tailscaleIdentitySteps(p *setupPlan) {
	have := r.identitySet()
	given := strings.TrimSpace(r.tailscaleClientID)
	text := "github: the Tailscale identity CI joins your tailnet with, the variables " + varTSClient + " and " + varTSAudience
	switch {
	case have && (given == "" || given == r.ghVars[varTSClient]):
		p.ok("github: the Tailscale identity, the variables " + varTSClient + " and " + varTSAudience)
		return
	case given == "" && r.ghVars[varTSClient] != "":
		// Half written: the audience is missing or is not the one of the client ID. The client ID is
		// there, so the audience is made again from it, which needs nothing from you.
		p.steps = append(p.steps, planStep{op: opChange, text: "github: the audience of the Tailscale identity, the variable " + varTSAudience + ", made again from the client ID in " + varTSClient, do: func(ctx context.Context) (string, error) {
			if err := r.gh.SetVariable(ctx, r.repo.Name, varTSAudience, tailscaleAudience(r.ghVars[varTSClient])); err != nil {
				return "", err
			}
			return "", nil
		}})
		return
	case given == "" && !stdinIsTerminal(r.in):
		p.cannot(text, r.tailscaleIdentityHow()+". Then run "+tailscaleClientCommand+": the client ID is not secret, so it may be given on a command line")
		return
	}
	op := opAdd
	if have {
		op = opChange
	}
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
	return "", nil
}

// identitySet says the repository has the identity: the client ID, and the audience made from it.
// An audience that is missing or is another one is a half written identity, which does not join.
func (r *setupRun) identitySet() bool {
	id := r.ghVars[varTSClient]
	return id != "" && r.ghVars[varTSAudience] == tailscaleAudience(id)
}

// tailscaleVariableSteps takes away the identity variables when deploy.connect is not tailscale
// any more.
func (r *setupRun) tailscaleVariableSteps(p *setupPlan) {
	if r.ghVars[varMarker] == "" || r.t.connectMode == connTailscale {
		return
	}
	repo := r.repo.Name
	var vars []string
	for _, n := range []string{varTSClient, varTSAudience} {
		if r.ghVars[n] != "" {
			vars = append(vars, n)
		}
	}
	if len(vars) == 0 {
		return
	}
	p.add(opRemove, "github: the variables "+strings.Join(vars, " and ")+", which tifa set and deploy.connect no longer needs", func(ctx context.Context) (string, error) {
		for _, n := range vars {
			if err := r.gh.DeleteVariable(ctx, repo, n); err != nil {
				return "", err
			}
		}
		return "", nil
	})
}
