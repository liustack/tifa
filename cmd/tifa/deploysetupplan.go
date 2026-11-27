package main

// tifa deploy setup --plan builds the plan setup builds, prints it and stops. It changes nothing
// and asks nothing, so a script, a CI job or an AI agent can run it. The exit status says what
// came of it, the same for one server or several:
//
//	0  everything matches tifa.yaml
//	2  the plan has something to do, or something the developer must do (a step setup cannot do)
//	1  the plan could not be made
//
// These are the meanings of terraform plan -detailed-exitcode. The statuses are fixed and tested:
// a plan whose text and status disagree is worse than none (Puppet PUP-686).

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	planExitMatches = 0
	planExitFailed  = 1
	planExitPending = 2
)

// checkPlanFlags refuses the flags that contradict --plan.
func checkPlanFlags(f setupFlags) error {
	switch {
	case f.plan && f.yes:
		return errors.New("--plan only prints the plan and changes nothing, and --yes applies it: use one of them")
	case f.asJSON && !f.plan:
		return errors.New("--json is for the plan: add --plan")
	}
	return nil
}

// pending says the plan has a step that adds, changes or removes something, or that setup cannot
// do. Notes are advice and do not count.
func (p setupPlan) pending() bool {
	for _, s := range p.steps {
		if s.op != opOK {
			return true
		}
	}
	return false
}

// The keys of the JSON plan. They are written in docs/build-and-deploy/deploy-setup.md and stay as
// they are.
type planStepJSON struct {
	Op   planOp `json:"op"`
	Text string `json:"text"`
	Why  string `json:"why,omitempty"`
}

type planNoteJSON struct {
	What string `json:"what"`
	Why  string `json:"why"`
}

type planServerJSON struct {
	Host    string         `json:"host"`
	Steps   []planStepJSON `json:"steps"`
	Notes   []planNoteJSON `json:"notes"`
	Matches bool           `json:"matches"`
}

type planReportJSON struct {
	App     string           `json:"app"`
	Servers []planServerJSON `json:"servers"`
	Matches bool             `json:"matches"`
}

// add puts the steps and notes of a plan in the entry of a server. The ok steps only come with all.
func (s *planServerJSON) add(p setupPlan, all bool) {
	for _, st := range p.steps {
		if st.op == opOK && !all {
			continue
		}
		s.Steps = append(s.Steps, planStepJSON{Op: st.op, Text: st.text, Why: st.why})
	}
	for _, n := range p.notes {
		s.Notes = append(s.Notes, planNoteJSON{What: n.what, Why: n.why})
	}
	s.Matches = s.Matches && !p.pending()
}

// deploySetupPlan prints the plan of every server and ends with the exit status above. A plan that
// cannot be made ends it with status 1 and an error, and with --json nothing is printed then:
// stdout holds one JSON object or nothing.
func deploySetupPlan(ctx context.Context, ts []deployTarget, f setupFlags, stdout, stderr io.Writer) error {
	// Notes of the app, such as the certificate that wins, go beside the JSON, not into it.
	text, buildOut := stdout, stdout
	if f.asJSON {
		text, buildOut = stderr, io.Discard
	}
	if len(ts) > 1 {
		if _, _, err := newSetupGroup(ctx, ts, &f); err != nil {
			return err
		}
	}
	report := planReportJSON{App: ts[0].app, Matches: true, Servers: make([]planServerJSON, len(ts))}
	one := func(t deployTarget) (setupPlan, *setupRun, error) {
		if err := checkSetupFlags(t, f); err != nil {
			return setupPlan{}, nil, err
		}
		cert, err := loadOwnCert(t)
		if err != nil {
			return setupPlan{}, nil, err
		}
		if f.cert = cert; cert != nil {
			sayOwnCertWins(t, text)
		}
		sayServerAddrUnused(t, text)
		// An empty input: nothing is asked, and no tool of this machine is installed or logged in.
		p, run, err := buildSetupPlan(ctx, t, f, false, strings.NewReader(""), buildOut, stderr)
		if err != nil {
			if len(ts) > 1 {
				err = fmt.Errorf("%s: %w", t.host, err)
			}
			return p, run, err
		}
		if run.haveServer && run.st.SSHPassword {
			p.notes = append(p.notes, planNote{what: "ssh: the server accepts passwords", why: "go tool tifa deploy harden turns that off"})
		}
		return p, run, nil
	}
	pending := false
	for i, t := range ts {
		p, run, err := one(t)
		if err != nil {
			return err
		}
		if f.group != nil {
			f.group.states[i] = run.st
		}
		server := planServerJSON{Host: t.host, Steps: []planStepJSON{}, Notes: []planNoteJSON{}, Matches: true}
		server.add(p, f.all)
		report.Servers[i] = server
		pending = pending || p.pending()
		if !f.asJSON {
			printPlanText(stdout, t, p, f.all, fmt.Sprintf("Plan for %s on %s%s%s", t.app, t.host, t.serverNote(), t.dest.fromHomeNote()), t.host+t.serverNote())
		}
	}
	// With several servers the round of GitHub Actions comes last, with every server.
	if ts[0].github && len(ts) > 1 {
		g := f.group
		g.github = true
		p, _, err := one(ts[0])
		if err != nil {
			return err
		}
		report.Servers[0].add(p, f.all)
		pending = pending || p.pending()
		if !f.asJSON {
			printPlanText(stdout, ts[0], p, f.all, fmt.Sprintf("Plan for %s on GitHub Actions, which deploys it to %s", ts[0].app, hostNames(ts)), "GitHub Actions")
		}
	}
	report.Matches = !pending
	if f.asJSON {
		if err := json.MarshalWrite(stdout, report, jsontext.WithIndent("  ")); err != nil {
			return err
		}
		fmt.Fprintln(stdout)
	} else if pending {
		fmt.Fprintln(stdout, "This was a plan (--plan): nothing was changed, and it differs from tifa.yaml.")
	} else if len(ts) > 1 {
		fmt.Fprintf(stdout, "Every server of %s matches tifa.yaml.\n", ts[0].app)
	}
	if pending {
		return &exitError{code: planExitPending}
	}
	return nil
}

// printPlanText prints one plan as setup does: the plan, or one line when all matches.
func printPlanText(w io.Writer, t deployTarget, p setupPlan, all bool, title, where string) {
	if !p.pending() && len(p.notes) == 0 && !all {
		fmt.Fprintf(w, "%s on %s matches tifa.yaml: %d things checked (--all lists them).\n", t.app, where, p.count(opOK))
		return
	}
	p.print(w, title, all)
}
