package main

// tifa deploy setup installs and logs in cf and gh on this machine, when tifa.yaml needs them:
// cf for deploy.front cloudflare (which deploy.ssh cloudflare needs too), and gh for deploy.ci
// github without TIFA_DEPLOY_GITHUB_TOKEN. Installing and logging in are steps of a plan of their
// own, and the developer's y to it is the consent. The rest of the plan reads Cloudflare and
// GitHub with the tools, so setup plans it after them and asks again.
//
// Setup installs only with a package manager that is on the machine already, and only after a y
// typed on the terminal, since a login opens a browser: --yes installs nothing. Tifa never
// downloads a program itself. Otherwise the layer that needs the tool lists the commands among
// what the developer does.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The programs setup runs on this machine to install and log in cf and gh. Tests replace them.
var (
	// lookProgram finds a program by name on the PATH, or checks a path.
	lookProgram = exec.LookPath
	// runAttached runs a program on the terminal of the developer.
	runAttached = func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		c := exec.CommandContext(ctx, name, args...)
		c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
		return c.Run()
	}
	// programOutput runs a program and returns what it printed. The error holds what it printed
	// on stderr.
	programOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := exec.CommandContext(ctx, name, args...).Output()
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			err = fmt.Errorf("%w: %s", err, oneLine(string(exit.Stderr)))
		}
		return out, err
	}
	// toolOS is the system setup runs on.
	toolOS = runtime.GOOS
)

// cfProgram and ghProgram are what setup runs as cf and gh: the name, or the path of the one it
// just installed, which the PATH of this process may not find. A package manager adds its folder
// to the PATH of terminals opened later, if at all.
var (
	cfProgram = "cf"
	ghProgram = "gh"
)

// ghLoginArgs log gh in through the browser. The host, the way of logging in and the git protocol
// are given, so gh asks only whether to log git in with GitHub too, and either answer works for
// tifa. The protocol is https: ssh would change how gh clones every other repository, and https
// is what gh uses when nobody set it. The scopes gh asks for by default (repo, read:org, gist)
// cover the secrets and variables of a repository. A yes adds workflow, which git needs to push
// the workflow file with the login of gh.
var ghLoginArgs = []string{"auth", "login", "--hostname", "github.com", "--web", "--git-protocol", "https"}

// ghLinuxInstall is GitHub's page on installing gh on Linux, where it needs root and GitHub's
// package repository.
const ghLinuxInstall = "https://github.com/cli/cli/blob/trunk/docs/install_linux.md"

// toolSteps plans installing and logging in cf and gh, when tifa.yaml needs them and they are not
// ready. It sets the line that says the rest comes after them. Only a y typed on the terminal
// installs or logs in: with --yes, which an agent passes on a terminal too, it plans nothing.
func (r *setupRun) toolSteps(ctx context.Context, p *setupPlan) {
	if r.yes || !stdinIsTerminal(r.in) {
		return
	}
	var tools, parts []string
	if r.t.domain != "" && !r.useCF(ctx) && r.cfSteps(p) {
		tools, parts = append(tools, "cf"), append(parts, "Cloudflare")
	}
	if r.needGH() && !ghLoggedIn(ctx, r.t.root) && r.ghSteps(p) {
		tools, parts = append(tools, "gh"), append(parts, "GitHub")
	}
	if len(tools) == 0 {
		return
	}
	p.then = fmt.Sprintf("Setup plans the rest, with the %s %s, once %s %s ready, and asks again.",
		strings.Join(parts, " and "), plural(len(parts), "part", "parts"), strings.Join(tools, " and "), plural(len(tools), "is", "are"))
}

// toolStep adds a step on this machine. It may fail: setup then goes on with what does not need
// the tool, and the next plan lists it for the developer.
func toolStep(p *setupPlan, what string, do func(context.Context) (string, error)) {
	p.steps = append(p.steps, planStep{op: opAdd, text: "this machine: " + what, soft: true, do: do})
}

// cfSteps plans cf: installed with pnpm, then logged in. It plans nothing without cf and pnpm.
func (r *setupRun) cfSteps(p *setupPlan) bool {
	if !haveCF() {
		if _, err := lookProgram("pnpm"); err != nil {
			return false
		}
		toolStep(p, "install cf with "+cfInstallHow, r.installCF)
	}
	toolStep(p, "log in to cf (opens a browser)", func(ctx context.Context) (string, error) {
		if !haveCF() {
			return "", skipped{"cf is not installed"}
		}
		return "", r.login(ctx, cfProgram, "cf auth login", "auth", "login")
	})
	return true
}

// installCF installs cf with pnpm into the global bin folder of pnpm, which needs pnpm setup to
// have run once.
func (r *setupRun) installCF(ctx context.Context) (string, error) {
	byHand := ". To install cf by hand: " + cfInstallHow
	out, err := programOutput(ctx, "pnpm", "bin", "--global")
	if err != nil {
		return "", fmt.Errorf("pnpm bin --global: %v%s", err, byHand)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", errors.New("pnpm has no global bin directory: run pnpm setup, open a new terminal and run tifa deploy setup again. Or install cf by hand: " + cfInstallHow)
	}
	if err := r.install(ctx, "pnpm", "add", "--global", "cf"); err != nil {
		return "", fmt.Errorf("%v%s", err, byHand)
	}
	path, err := installed("cf", filepath.Join(dir, "cf"))
	if err != nil {
		return "", err
	}
	cfProgram = path
	return path, nil
}

// needGH says the GitHub part needs gh: deploy.ci is github, and no token stands in for gh.
func (r *setupRun) needGH() bool {
	if !r.t.github {
		return false
	}
	token, _, err := readDeployInput(r.t.root, "", "", githubTokenEnv)
	// A .env that cannot be read fails the GitHub part, which says so.
	return err == nil && token == ""
}

// ghLoggedIn says gh is installed and logged in.
func ghLoggedIn(ctx context.Context, dir string) bool {
	if !haveGH() {
		return false
	}
	_, err := readGH(ctx, dir, nil, "auth", "status")
	return err == nil
}

// ghSteps plans gh: installed with the package manager of the system, then logged in. It plans
// nothing without gh and that package manager.
func (r *setupRun) ghSteps(p *setupPlan) bool {
	if !haveGH() {
		manager := ghManager()
		if manager == nil {
			return false
		}
		toolStep(p, "install gh with "+strings.Join(manager, " "), r.installGH)
	}
	toolStep(p, "log in to gh (opens a browser)", func(ctx context.Context) (string, error) {
		if !haveGH() {
			return "", skipped{"gh is not installed"}
		}
		return "", r.login(ctx, ghProgram, "gh auth login", ghLoginArgs...)
	})
	return true
}

// ghManager is the command that installs gh with a package manager of this machine: brew on
// macOS, winget on Windows. It is nil elsewhere, Linux included, where gh needs root and GitHub's
// package repository.
func ghManager() []string {
	has := func(name string) bool { _, err := lookProgram(name); return err == nil }
	switch {
	case toolOS == "darwin" && has("brew"):
		return []string{"brew", "install", "gh"}
	case toolOS == "windows" && has("winget"):
		return []string{"winget", "install", "--id", "GitHub.cli", "-e"}
	}
	return nil
}

// ghInstallHow says how the developer installs gh on this machine.
func ghInstallHow() string {
	switch m := ghManager(); {
	case m != nil:
		return "install it with " + strings.Join(m, " ")
	case toolOS == "linux":
		return "install it as " + ghLinuxInstall + " says"
	}
	return "install it from https://cli.github.com/"
}

func (r *setupRun) installGH(ctx context.Context) (string, error) {
	manager := ghManager()
	if err := r.install(ctx, manager[0], manager[1:]...); err != nil {
		return "", fmt.Errorf("%v. To install gh by hand: %s", err, strings.Join(manager, " "))
	}
	// Where the package manager puts gh. winget does not change the PATH of this process.
	var at string
	switch toolOS {
	case "darwin":
		if out, err := programOutput(ctx, "brew", "--prefix"); err == nil {
			at = filepath.Join(strings.TrimSpace(string(out)), "bin", "gh")
		}
	case "windows":
		at = filepath.Join(os.Getenv("ProgramFiles"), "GitHub CLI", "gh.exe")
	}
	path, err := installed("gh", at)
	if err != nil {
		return "", err
	}
	ghProgram = path
	return path, nil
}

// install runs a package manager on the terminal, which shows what it does and may ask.
func (r *setupRun) install(ctx context.Context, name string, args ...string) error {
	cmd := strings.Join(append([]string{name}, args...), " ")
	fmt.Fprintf(r.stdout, "  running %s\n", cmd)
	if err := runAttached(ctx, r.in, r.stdout, r.errOut, name, args...); err != nil {
		return fmt.Errorf("%s failed (%v), see above", cmd, err)
	}
	return nil
}

// login runs the login of a tool on the terminal. byHand is the command the developer runs when it
// fails.
func (r *setupRun) login(ctx context.Context, program, byHand string, args ...string) error {
	fmt.Fprintf(r.stdout, "  running %s\n", byHand)
	if err := runAttached(ctx, r.in, r.stdout, r.errOut, program, args...); err != nil {
		return fmt.Errorf("%s failed (%v), see above. To log in by hand: %s", byHand, err, byHand)
	}
	return nil
}

// installed finds the program a package manager just installed: at path, where it goes, else on
// the PATH.
func installed(name, path string) (string, error) {
	if path != "" {
		if p, err := lookProgram(path); err == nil {
			return p, nil
		}
	}
	if p, err := lookProgram(name); err == nil {
		return p, nil
	}
	where := "on the PATH"
	if path != "" {
		where = "at " + path + " or on the PATH"
	}
	return "", fmt.Errorf("%s was installed, but setup cannot find it %s: open a new terminal and run tifa deploy setup again", name, where)
}
