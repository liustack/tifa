package main

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fakeMachine stands in for the programs of the developer's machine: which ones are there, what
// the package managers print, and what ran with the terminal attached. Nothing is installed and
// nobody logs in for real.
type fakeMachine struct {
	os string
	// files are the programs on the machine by path, onPath the ones the PATH finds by name.
	files  map[string]bool
	onPath map[string]string
	// pnpmBin is what pnpm bin --global prints.
	pnpmBin string
	// fail makes the command line fail.
	fail map[string]error
	ran  []string
	cf   *fakeCF
	gh   *fakeGH
}

const fakePNPMBin = "/home/dev/.local/share/pnpm"

func newFakeMachine(goos string, programs ...string) *fakeMachine {
	m := &fakeMachine{os: goos, files: map[string]bool{}, onPath: map[string]string{}, pnpmBin: fakePNPMBin, fail: map[string]error{}}
	for _, p := range programs {
		m.onPath[p] = "/usr/local/bin/" + p
		m.files["/usr/local/bin/"+p] = true
	}
	return m
}

// install puts m in place of the machine. Install the fakes of cf and gh first: it takes over
// haveCF and haveGH.
func (m *fakeMachine) install(t *testing.T) {
	t.Helper()
	oldLook, oldRun, oldOut, oldOS := lookProgram, runAttached, programOutput, toolOS
	oldHaveCF, oldHaveGH, oldCF, oldGH := haveCF, haveGH, cfProgram, ghProgram
	t.Cleanup(func() {
		lookProgram, runAttached, programOutput, toolOS = oldLook, oldRun, oldOut, oldOS
		haveCF, haveGH, cfProgram, ghProgram = oldHaveCF, oldHaveGH, oldCF, oldGH
	})
	toolOS = m.os
	t.Setenv("ProgramFiles", `C:\Program Files`)
	lookProgram = func(name string) (string, error) {
		fakeMu.Lock()
		defer fakeMu.Unlock()
		if p, ok := m.onPath[name]; ok {
			return p, nil
		}
		if m.files[name] {
			return name, nil
		}
		return "", exec.ErrNotFound
	}
	haveCF = func() bool { _, err := lookProgram(cfProgram); return err == nil }
	haveGH = func() bool { _, err := lookProgram(ghProgram); return err == nil }
	programOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch strings.Join(append([]string{name}, args...), " ") {
		case "pnpm bin --global":
			return []byte(m.pnpmBin + "\n"), nil
		case "brew --prefix":
			return []byte("/opt/homebrew\n"), nil
		}
		return nil, errors.New("fake machine: unknown command")
	}
	runAttached = func(_ context.Context, _ io.Reader, _, _ io.Writer, name string, args ...string) error {
		fakeMu.Lock()
		defer fakeMu.Unlock()
		line := strings.Join(append([]string{name}, args...), " ")
		m.ran = append(m.ran, line)
		if err := m.fail[line]; err != nil {
			return err
		}
		switch line {
		case "pnpm add --global cf":
			m.files[filepath.Join(m.pnpmBin, "cf")] = true
		case "brew install gh":
			m.files[filepath.Join("/opt/homebrew", "bin", "gh")] = true
		case "winget install --id GitHub.cli -e":
			m.files[filepath.Join(`C:\Program Files`, "GitHub CLI", "gh.exe")] = true
		}
		switch {
		case strings.HasSuffix(line, "cf auth login") && m.cf != nil:
			m.cf.loggedOut = false
		case strings.HasSuffix(line, " "+strings.Join(ghLoginArgs, " ")) && m.gh != nil:
			m.gh.loggedIn = true
		}
		return nil
	}
}

// mentionsTool says the text names cf or gh as a word.
var mentionsTool = regexp.MustCompile(`\b(cf|gh)\b`)

// With the default deploy settings nothing about cf or gh ever shows, and nothing runs.
func TestSetupToolsDefaultSettings(t *testing.T) {
	root := setupApp(t, "deploy:\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	m := newFakeMachine("darwin", "pnpm", "brew")
	m.install(t)
	t.Setenv(githubTokenEnv, "")
	out, err := runSetup(t, root, true, "y\n", "--all")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if mentionsTool.MatchString(out) || len(m.ran) != 0 {
		t.Errorf("ran %v\n%s", m.ran, out)
	}
}

// front: cloudflare without cf, and pnpm there: the first plan installs cf and logs it in, and
// says the Cloudflare part comes after. After y both ran, and setup planned the Cloudflare part
// and asked again.
func TestSetupInstallsCF(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	cf := cfSetup(t, "example.com")
	cf.loggedOut = true
	m := newFakeMachine("darwin", "pnpm")
	m.cf = cf.fakeCF
	m.install(t)

	out, err := runSetup(t, root, true, "y\ny\n")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	first, rest, ok := strings.Cut(out, "Apply this plan? [y/N]")
	if !ok {
		t.Fatalf("no question:\n%s", out)
	}
	for _, want := range []string{
		"add     this machine: install cf with pnpm add --global cf",
		"add     this machine: log in to cf (opens a browser)",
		"Setup plans the rest, with the Cloudflare part, once cf is ready, and asks again.",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("first plan lacks %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "cloudflare:") || strings.Contains(first, "You need to do") {
		t.Errorf("the first plan has more than the tools:\n%s", first)
	}
	cfPath := filepath.Join(fakePNPMBin, "cf")
	if want := []string{"pnpm add --global cf", cfPath + " auth login"}; !slices.Equal(m.ran, want) {
		t.Errorf("ran %q, want %q", m.ran, want)
	}
	if cfProgram != cfPath {
		t.Errorf("cf is %q, want the one pnpm installed, %q", cfProgram, cfPath)
	}
	for _, want := range []string{
		"done    this machine: install cf with pnpm add --global cf",
		"done    this machine: log in to cf (opens a browser)",
		"add     cloudflare: the tunnel of the server, tifa-vps-1234",
		"Apply this plan? [y/N]",
	} {
		if !strings.Contains(rest, want) {
			t.Errorf("lacks %q after the first answer:\n%s", want, rest)
		}
	}
	if !slices.Contains(cf.writes(), "tunnels create --name tifa-vps-1234 --config-source cloudflare") {
		t.Errorf("the Cloudflare part did not run: %v", cf.writes())
	}
}

// Without pnpm setup cannot install cf: the plan lists the command for the developer.
func TestSetupNoPNPM(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n", nil)
	newSetupServer(t, "", factsOf("shop", 9531, ""))
	m := newFakeMachine("darwin", "brew")
	m.install(t)
	out, err := runSetup(t, root, true, "n\n")
	if err == nil || len(m.ran) != 0 || strings.Contains(out, "this machine:") || !strings.Contains(out, "cf is not installed: pnpm add --global cf, then cf auth login") {
		t.Fatalf("%v, ran %v\n%s", err, m.ran, out)
	}
}

// ci: github with TIFA_DEPLOY_GITHUB_TOKEN: the token does the work, and gh is not mentioned.
func TestSetupTokenNeedsNoGH(t *testing.T) {
	root := setupApp(t, "deploy:\n  ci: github\n", nil)
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, ""))
	g := newFakeGH()
	g.installREST(t)
	m := newFakeMachine("darwin", "brew", "pnpm")
	m.install(t)
	out, err := runSetup(t, root, true, "y\n")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if mentionsTool.MatchString(out) || len(m.ran) != 0 || g.vars[varMarker] == "" {
		t.Errorf("ran %v, vars %v\n%s", m.ran, g.vars, out)
	}
}

// gh is installed with the package manager of the system: brew on macOS, winget on Windows. On
// Linux setup only lists how.
func TestSetupInstallsGH(t *testing.T) {
	for _, c := range []struct {
		os, manager, install, path string
	}{
		{"darwin", "brew", "brew install gh", filepath.Join("/opt/homebrew", "bin", "gh")},
		{"windows", "winget", "winget install --id GitHub.cli -e", filepath.Join(`C:\Program Files`, "GitHub CLI", "gh.exe")},
	} {
		t.Run(c.os, func(t *testing.T) {
			root := setupApp(t, "deploy:\n  ci: github\n", nil)
			newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, ""))
			gh := newFakeGH()
			gh.loggedIn = false
			gh.install(t)
			t.Setenv(githubTokenEnv, "")
			m := newFakeMachine(c.os, c.manager)
			m.gh = gh
			m.install(t)

			out, err := runSetup(t, root, true, "y\ny\n")
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			first, _, _ := strings.Cut(out, "Apply this plan?")
			for _, want := range []string{
				"add     this machine: install gh with " + c.install,
				"add     this machine: log in to gh (opens a browser)",
				"Setup plans the rest, with the GitHub part, once gh is ready, and asks again.",
			} {
				if !strings.Contains(first, want) {
					t.Errorf("first plan lacks %q:\n%s", want, first)
				}
			}
			// https, so gh leaves the git protocol of the developer's other repositories alone.
			login := c.path + " auth login --hostname github.com --web --git-protocol https"
			if want := []string{c.install, login}; !slices.Equal(m.ran, want) {
				t.Errorf("ran %q, want %q", m.ran, want)
			}
			if ghProgram != c.path {
				t.Errorf("gh is %q, want %q", ghProgram, c.path)
			}
			if !strings.Contains(out, "add     github: a key for acme/shop") || gh.vars[varMarker] == "" {
				t.Errorf("the GitHub part did not run: %v\n%s", gh.vars, out)
			}
		})
	}
	t.Run("linux", func(t *testing.T) {
		root := setupApp(t, "deploy:\n  ci: github\n", nil)
		newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, ""))
		newFakeGH().install(t)
		t.Setenv(githubTokenEnv, "")
		m := newFakeMachine("linux", "pnpm")
		m.install(t)
		out, err := runSetup(t, root, true, "n\n")
		if err == nil || len(m.ran) != 0 || strings.Contains(out, "this machine:") ||
			!strings.Contains(out, "gh, the GitHub CLI, is not installed: install it as "+ghLinuxInstall+" says, then run gh auth login") {
			t.Fatalf("%v, ran %v\n%s", err, m.ran, out)
		}
	})
}

// Without a terminal nothing is installed and nobody logs in, even with --yes: the commands go to
// the list of what the developer does.
func TestSetupToolsNoTerminal(t *testing.T) {
	root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n  ci: github\n", nil)
	newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, ""))
	newFakeGH().install(t)
	t.Setenv(githubTokenEnv, "")
	m := newFakeMachine("darwin", "pnpm", "brew")
	m.install(t)
	out, err := runSetup(t, root, false, "", "--yes")
	if err == nil || len(m.ran) != 0 || strings.Contains(out, "this machine:") {
		t.Fatalf("%v, ran %v\n%s", err, m.ran, out)
	}
	for _, want := range []string{"cf is not installed: pnpm add --global cf, then cf auth login", "gh, the GitHub CLI, is not installed: install it with brew install gh, then run gh auth login"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

// An install that fails is reported with the way to do it by hand, the login of that tool is
// skipped, and setup goes on with what does not need it.
func TestSetupToolInstallFails(t *testing.T) {
	for _, c := range []struct {
		name, pnpmBin, why string
		fail               bool
	}{
		{"pnpm add fails", fakePNPMBin, "pnpm add --global cf failed (exit status 1), see above. To install cf by hand: pnpm add --global cf", true},
		{"no global bin", "", "pnpm has no global bin directory: run pnpm setup, open a new terminal and run tifa deploy setup again. Or install cf by hand: pnpm add --global cf", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := setupApp(t, "site:\n  url: https://shop.example.com\ndeploy:\n  front: cloudflare\n  ci: github\n", nil)
			newSetupServer(t, "hostname 203.0.113.7\n", factsOf("shop", 9531, ""))
			gh := newFakeGH()
			gh.loggedIn = false
			gh.install(t)
			t.Setenv(githubTokenEnv, "")
			m := newFakeMachine("darwin", "pnpm", "brew")
			m.gh, m.pnpmBin = gh, c.pnpmBin
			if c.fail {
				m.fail["pnpm add --global cf"] = errors.New("exit status 1")
			}
			m.install(t)

			out, err := runSetup(t, root, true, "y\ny\n")
			if err == nil || !strings.Contains(err.Error(), "needs you") {
				t.Fatalf("%v\n%s", err, out)
			}
			for _, want := range []string{
				"warning this machine: install cf with pnpm add --global cf: " + c.why,
				"skipped this machine: log in to cf (opens a browser): cf is not installed",
				"done    this machine: install gh with brew install gh",
				"done    this machine: log in to gh (opens a browser)",
				"cf is not installed: pnpm add --global cf, then cf auth login",
				"add     github: a key for acme/shop",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("lacks %q:\n%s", want, out)
				}
			}
			if slices.ContainsFunc(m.ran, func(s string) bool { return strings.HasSuffix(s, "cf auth login") }) {
				t.Errorf("cf logged in without cf: %v", m.ran)
			}
			if !c.fail && slices.Contains(m.ran, "pnpm add --global cf") {
				t.Errorf("pnpm add ran without a global bin directory: %v", m.ran)
			}
			if gh.vars[varMarker] == "" {
				t.Errorf("the GitHub part did not run")
			}
		})
	}
}
