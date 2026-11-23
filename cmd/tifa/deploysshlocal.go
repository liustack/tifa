package main

// What tifa deploy setup does on the machine of the developer for deploy.ssh cloudflare, so the
// ssh of this machine goes through the Cloudflare Tunnel and the server needs no open ssh port
// for anyone. The steps are items of the plan, applied with the answer to the plan:
//
//   - install cloudflared, when it is not on the PATH, with brew or winget. Setup never removes it.
//   - write the block of the ssh config that makes ssh run cloudflared for the ssh host name.
//   - check that ssh reaches tifa@<ssh host> through the tunnel. The first time cloudflared opens
//     the Cloudflare Access login in the browser and waits for it, which is the one thing a person
//     does by hand.
//   - once that works, point TIFA_DEPLOY_HOST at the ssh host, so deploys from this machine go
//     through the tunnel. It changes the .env of the app when the host came from there. When it
//     came from the environment or a flag, setup says what to set and leaves it.
//
// The first setup of a server reaches it by the address the developer gave, as root, since the
// tunnel does not exist yet. The switch comes last, and only after the check passed.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// cloudflaredPage is where Cloudflare lists the installers of cloudflared for every system.
const cloudflaredPage = "https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/"

// tunnelCheckTimeout is how long the check of the tunnel waits for the Access login in the
// browser. Tests set it.
var tunnelCheckTimeout = 4 * time.Minute

// cloudflaredManager is the command that installs cloudflared with a package manager of this
// machine: brew on macOS, winget on Windows. It is nil elsewhere, Linux included, where the way
// depends on the distribution.
func cloudflaredManager() []string {
	has := func(name string) bool { _, err := lookProgram(name); return err == nil }
	switch {
	case toolOS == "darwin" && has("brew"):
		return []string{"brew", "install", "cloudflared"}
	case toolOS == "windows" && has("winget"):
		return []string{"winget", "install", "--id", "Cloudflare.cloudflared", "-e"}
	}
	return nil
}

// cloudflaredHow says how the developer installs cloudflared when setup does not.
func cloudflaredHow() string {
	switch toolOS {
	case "darwin":
		return "Install it with brew install cloudflared (https://brew.sh), or from " + cloudflaredPage
	case "windows":
		return "Install it with winget install --id Cloudflare.cloudflared -e, or from " + cloudflaredPage
	}
	return "Install it from " + cloudflaredPage + ", with the package of your distribution"
}

// cloudflaredNow finds cloudflared: on the PATH, or where setup just installed it.
func (r *setupRun) cloudflaredNow() (string, bool) {
	if r.cloudflaredAt != "" {
		return r.cloudflaredAt, true
	}
	p, err := lookProgram("cloudflared")
	return p, err == nil
}

// installCloudflared runs the package manager the plan found and finds the program it put in.
func (r *setupRun) installCloudflared(manager []string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if err := r.install(ctx, manager[0], manager[1:]...); err != nil {
			return "", fmt.Errorf("%v. To install cloudflared by hand: %s", err, strings.Join(manager, " "))
		}
		// Where the package manager puts it. winget does not change the PATH of this process.
		var at string
		switch toolOS {
		case "darwin":
			if out, err := programOutput(ctx, "brew", "--prefix"); err == nil {
				at = filepath.Join(strings.TrimSpace(string(out)), "bin", "cloudflared")
			}
		case "windows":
			at = filepath.Join(os.Getenv("ProgramFiles(x86)"), "cloudflared", "cloudflared.exe")
		}
		path, err := installed("cloudflared", at)
		if err != nil {
			return "", err
		}
		r.cloudflaredAt = path
		return path, nil
	}
}

// localSSHSteps plans cloudflared and the block of the ssh config for host, and, unless switching
// is false, the check of the tunnel and the move of TIFA_DEPLOY_HOST. It returns false when the
// machine cannot be made ready by setup: the plan then holds what the developer does.
func (r *setupRun) localSSHSteps(p *setupPlan, host, shown string, switching bool) {
	t := r.t
	path, have := r.cloudflaredNow()
	switch manager := cloudflaredManager(); {
	case have:
		p.ok("this machine: cloudflared at " + path)
	case manager != nil:
		p.steps = append(p.steps, planStep{op: opAdd, text: "this machine: install cloudflared with " + strings.Join(manager, " "),
			soft: true, do: r.installCloudflared(manager)})
	default:
		p.cannot("this machine: cloudflared, which ssh runs to reach the server through the tunnel", cloudflaredHow())
		return
	}

	cfgPath, err := sshConfigPath()
	if err != nil {
		p.cannot("this machine: the ssh config for the tunnel", err.Error())
		return
	}
	text := fmt.Sprintf("this machine: a block for %s in %s, so ssh goes through the tunnel (the other lines of the file stay)", host, cfgPath)
	op := opAdd
	switch {
	case have:
		same, present, err := sshBlockState(cfgPath, t.app, host, path)
		switch {
		case err != nil:
			p.cannot("this machine: the ssh config for the tunnel", err.Error())
			return
		case same:
			p.ok(text)
			op = opOK
		case present:
			op = opChange
		}
	default:
		if present, err := hasSSHBlock(cfgPath, t.app); err == nil && present {
			op = opChange
		}
	}
	if op != opOK {
		p.steps = append(p.steps, planStep{op: op, text: text, soft: true, do: func(context.Context) (string, error) {
			path, ok := r.cloudflaredNow()
			if !ok {
				return "", skipped{"cloudflared is not installed"}
			}
			return "", writeSSHBlock(cfgPath, t.app, host, path)
		}})
	}
	if !switching {
		return
	}
	r.hostSwitchSteps(p, host, shown)
}

// hostSwitchSteps plans the check of the tunnel and the move of TIFA_DEPLOY_HOST to the ssh host.
func (r *setupRun) hostSwitchSteps(p *setupPlan, host, shown string) {
	t := r.t
	if t.dest.name == host {
		p.ok("this machine: TIFA_DEPLOY_HOST is the ssh host, so deploys go through the tunnel")
		return
	}
	if t.dest.from != dotenvHostFrom {
		// The environment and a flag are not setup's to change.
		p.notes = append(p.notes, planNote{
			what: "this machine: set TIFA_DEPLOY_HOST=" + shown + " to deploy through the tunnel",
			why:  fmt.Sprintf("Your TIFA_DEPLOY_HOST comes from the %s, which setup does not change. Set it to %s, or write it in the .env at the root of the app. Then the server needs no open ssh port", t.dest.fromText(), shown),
		})
		r.hostAdvice = fmt.Sprintf("To deploy from this machine through the tunnel, set TIFA_DEPLOY_HOST=%s (yours comes from the %s, which setup does not change).", host, t.dest.fromText())
		return
	}
	checked := false
	p.steps = append(p.steps, planStep{op: opAdd, soft: true,
		text: fmt.Sprintf("this machine: check that ssh reaches %s@%s through the tunnel (the first time, cloudflared opens the Cloudflare Access login in your browser and waits for you)", deployUser, host),
		do: func(ctx context.Context) (string, error) {
			if err := r.checkTunnel(ctx, host); err != nil {
				return "", err
			}
			checked = true
			return "", nil
		}})
	p.steps = append(p.steps, planStep{op: opChange, soft: true,
		text: fmt.Sprintf("this machine: TIFA_DEPLOY_HOST in .env, %s -> %s", t.host, host),
		do: func(context.Context) (string, error) {
			if !checked {
				return "", skipped{"ssh did not reach the server through the tunnel, so deploys keep going to " + t.host}
			}
			return "", setEnvHost(filepath.Join(t.root, ".env"), host)
		}})
}

// checkTunnel runs ssh to the ssh host through the block of the ssh config, as setup will use it
// from now on. It has no terminal to ask on (BatchMode), and the login of Cloudflare Access opens
// in the browser: cloudflared prints its address on stderr too, which goes to the developer, so a
// person who reads it for a coding agent can open it.
func (r *setupRun) checkTunnel(ctx context.Context, host string) error {
	fmt.Fprintf(r.stdout, "          Checking ssh to %s@%s through the tunnel. The first time, cloudflared opens the Cloudflare Access login in your browser and waits for you to finish it, for up to %s.\n",
		deployUser, host, tunnelCheckTimeout)
	ctx, cancel := context.WithTimeout(ctx, tunnelCheckTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, sshProgram, "-l", deployUser, "-o", "BatchMode=yes", "--", host, "true")
	c.Stdout, c.Stderr = io.Discard, r.errOut
	c.WaitDelay = sshWaitDelay
	err := c.Run()
	switch {
	case err == nil:
		return nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("no answer from %s in %s: finish the Cloudflare Access login in your browser, then run go tool tifa deploy setup again", host, tunnelCheckTimeout)
	}
	return fmt.Errorf("ssh %s@%s through the tunnel failed (%v), see above. The Access login does not put your ssh key on the server: it must be in /srv/tifa/.ssh/authorized_keys. Run go tool tifa deploy setup again once it works", deployUser, host, err)
}

// setEnvHost sets TIFA_DEPLOY_HOST in the .env at path, keeping every other line and the line
// endings of the file.
func setEnvHost(path, host string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out := withEnv(string(data), deployHostEnv, host)
	if strings.Contains(string(data), "\r\n") {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return os.WriteFile(path, []byte(out), 0o600)
}

// looksLikeSSHHost says host is a name of the kind setup makes for the ssh host of domain, without
// asking Cloudflare for the zone.
func looksLikeSSHHost(host, domain string) bool {
	return domain != "" && (isSSHHost(host, domain, domain) || isSSHHost(host, domain, ""))
}

// tunnelReady says the machine has what a connection to host through the tunnel needs: cloudflared
// and the block of the ssh config.
func (r *setupRun) tunnelReady() bool {
	if _, ok := r.cloudflaredNow(); !ok {
		return false
	}
	path, err := sshConfigPath()
	if err != nil {
		return false
	}
	present, err := hasSSHBlock(path, r.t.app)
	return err == nil && present
}

// machineFirst plans, ahead of everything else, what this machine needs to reach a server that
// the developer already names by its ssh host: another person's setup, or a second run. Without
// it the first ssh of setup could not get through the tunnel. It plans nothing when the target is
// not the ssh host of Cloudflare.
func (r *setupRun) machineFirst(ctx context.Context, p *setupPlan) error {
	t := r.t
	if t.sshMode != connCloudflare || !looksLikeSSHHost(t.dest.name, t.domain) || !r.useCF(ctx) {
		return nil
	}
	zone, err := findZone(ctx, t.domain)
	if err != nil {
		return nil
	}
	found, err := findSSHHosts(ctx, "", t.domain, zone.Name)
	if err != nil || found.host != t.dest.name {
		return nil
	}
	r.sshName = found.host
	if err := r.peopleSteps(ctx, p, found.host); err != nil {
		return err
	}
	r.localSSHSteps(p, found.host, found.host, false)
	if p.todo() > 0 {
		p.then = "Setup plans the rest once this machine can reach the server through the tunnel, and asks again."
	}
	return nil
}
