package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type ciFile struct {
	Name string `yaml:"name"`
	On   struct {
		PullRequest any `yaml:"pull_request"`
		Push        struct {
			Branches []string `yaml:"branches"`
		} `yaml:"push"`
	} `yaml:"on"`
	Jobs map[string]struct {
		Steps []struct {
			Uses string            `yaml:"uses"`
			With map[string]any    `yaml:"with"`
			Run  string            `yaml:"run"`
			Env  map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readCI(t *testing.T, dir string) (text string, ci ciFile) {
	t.Helper()
	text = readText(t, filepath.Join(dir, filepath.FromSlash(ciPath)))
	if err := yaml.Unmarshal([]byte(text), &ci); err != nil {
		t.Fatalf("%s is not YAML: %v\n%s", ciPath, err, text)
	}
	return text, ci
}

// ciSteps returns "uses" or "run" of each step of the one job, in order.
func ciSteps(t *testing.T, ci ciFile) []string {
	t.Helper()
	if len(ci.Jobs) != 1 {
		t.Fatalf("want one job, got %d", len(ci.Jobs))
	}
	var out []string
	for _, job := range ci.Jobs {
		for _, s := range job.Steps {
			if s.Uses != "" {
				out = append(out, s.Uses)
			} else {
				out = append(out, "run: "+s.Run)
			}
		}
	}
	return out
}

const govulncheckStep = "go run golang.org/x/vuln/cmd/govulncheck@latest ./..."

// goMinorOf returns the minor of the go line of go.mod in dir, such as 1.27.
func goMinorOf(t *testing.T, dir string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^go (\d+\.\d+)`).FindStringSubmatch(readText(t, filepath.Join(dir, "go.mod")))
	if m == nil {
		t.Fatalf("go.mod has no go line:\n%s", readText(t, filepath.Join(dir, "go.mod")))
	}
	return m[1]
}

func TestNewWritesTheCIWorkflow(t *testing.T) {
	for _, empty := range []bool{false, true} {
		withPnpm(t, true)
		dir := filepath.Join(t.TempDir(), "app")
		args := []string{"new", dir}
		if empty {
			args = append(args, "--empty")
		}
		if _, _, err := run(t, args...); err != nil {
			t.Fatal(err)
		}
		text, ci := readCI(t, dir)
		if !strings.HasPrefix(text, "# tifa wrote this file") || !strings.Contains(strings.SplitN(text, "\n", 2)[0], "yours to edit") {
			t.Errorf("the first line does not say who wrote the file:\n%s", text)
		}
		if ci.On.PullRequest == nil && !strings.Contains(text, "pull_request") || len(ci.On.Push.Branches) != 1 || ci.On.Push.Branches[0] != "main" {
			t.Errorf("the workflow does not run on pull requests and pushes to main:\n%s", text)
		}
		got := strings.Join(ciSteps(t, ci), "\n")
		want := strings.Join([]string{
			"actions/checkout@v7", "actions/setup-go@v7", "pnpm/action-setup@v6", "actions/setup-node@v7",
			"run: pnpm install --frozen-lockfile --ignore-scripts", "run: go tool tifa check", "run: " + govulncheckStep,
		}, "\n")
		if got != want {
			t.Errorf("steps:\n%s\nwant:\n%s", got, want)
		}
		for _, job := range ci.Jobs {
			// go-version-file would install the exact patch of the go line (1.27.0), which has none of
			// the later fixes of net/http. The minor with an x and check-latest gets the newest patch.
			if w := job.Steps[1].With; w["go-version"] != goMinorOf(t, dir)+".x" || w["check-latest"] != true || w["go-version-file"] != nil {
				t.Errorf("setup-go does not install the newest patch of the minor of go.mod: %v", w)
			}
			if job.Steps[2].With["version"] != 10 && job.Steps[2].With["version"] != "10" {
				t.Errorf("pnpm/action-setup needs a version, package.json has no packageManager: %v", job.Steps[2].With)
			}
			if w := job.Steps[3].With; w["node-version"] != 24 || w["cache"] != "pnpm" {
				t.Errorf("setup-node: %v", w)
			}
		}
	}
}

func TestNewWorkflowUsesNpmWithoutPnpm(t *testing.T) {
	withPnpm(t, false)
	dir := filepath.Join(t.TempDir(), "app")
	if _, _, err := run(t, "new", dir); err != nil {
		t.Fatal(err)
	}
	text, ci := readCI(t, dir)
	got := strings.Join(ciSteps(t, ci), "\n")
	want := "actions/checkout@v7\nactions/setup-go@v7\nactions/setup-node@v7\nrun: npm ci\nrun: go tool tifa check\nrun: " + govulncheckStep
	if got != want || strings.Contains(text, "pnpm") {
		t.Errorf("steps:\n%s\nwant:\n%s\n%s", got, want, text)
	}
}

// Every command the workflow runs is one that AGENTS.md of the app documents.
func TestCIWorkflowRunsDocumentedCommands(t *testing.T) {
	withPnpm(t, true)
	dir := filepath.Join(t.TempDir(), "app")
	if _, _, err := run(t, "new", dir); err != nil {
		t.Fatal(err)
	}
	notes := readText(t, filepath.Join(dir, "AGENTS.md"))
	_, ci := readCI(t, dir)
	for _, step := range ciSteps(t, ci) {
		cmd, ok := strings.CutPrefix(step, "run: ")
		if !ok {
			continue
		}
		if cmd == govulncheckStep {
			continue // runs in CI only, an app does not run it by hand
		}
		name, _, _ := strings.Cut(cmd, " --")
		if !regexp.MustCompile("(?m)^\\| `" + regexp.QuoteMeta(name) + "` \\|").MatchString(notes) {
			t.Errorf("AGENTS.md has no command row for %q", name)
		}
	}
}

func TestAgentsWritesTheCIWorkflowWhenMissing(t *testing.T) {
	dir := existingApp(t, map[string]string{
		"pnpm-lock.yaml": "x",
		"package.json":   `{"name":"shop","packageManager":"pnpm@10.18.0"}`,
	})
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ciPath+": created") {
		t.Errorf("output does not say the workflow was written:\n%s", out)
	}
	text, ci := readCI(t, dir)
	// packageManager names pnpm, so pnpm/action-setup reads the version from it.
	if strings.Contains(text, "          version:") || !strings.Contains(strings.Join(ciSteps(t, ci), "\n"), "pnpm/action-setup@v6") {
		t.Errorf("workflow:\n%s", text)
	}
	// agents --check never asks for a file the app may have left out on purpose.
	if out, _, err := run(t, "agents", "--dir", dir, "--check"); err != nil {
		t.Errorf("check: %v\n%s", err, out)
	}
}

func TestAgentsLeavesAnExistingWorkflowAlone(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"ci.yml":  {ciPath: "name: mine\n"},
		"another": {".github/workflows/test.yaml": "name: tests\n"},
	} {
		dir := existingApp(t, files)
		before := snapshot(t, dir)
		if _, _, err := run(t, "agents", "--dir", dir); err != nil {
			t.Fatal(err)
		}
		for p, v := range before {
			if snapshot(t, dir)[p] != v {
				t.Errorf("%s: %s changed", name, p)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(ciPath))); name == "another" && err == nil {
			t.Errorf("%s: ci.yml written next to another workflow", name)
		}
	}
}

func TestAgentsWorkflowWithoutPackageJSON(t *testing.T) {
	dir := existingApp(t, nil)
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	_, ci := readCI(t, dir)
	if got := strings.Join(ciSteps(t, ci), "\n"); got != "actions/checkout@v7\nactions/setup-go@v7\nrun: go tool tifa check\nrun: "+govulncheckStep {
		t.Errorf("steps:\n%s", got)
	}
}

func TestAgentsWorkflowPnpmNeedsAVersionWithoutPackageManager(t *testing.T) {
	dir := existingApp(t, map[string]string{"pnpm-lock.yaml": "x", "package.json": `{"name":"shop"}`})
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	text, _ := readCI(t, dir)
	if !strings.Contains(text, "version: 10") {
		t.Errorf("no pnpm version:\n%s", text)
	}
}

func TestAgentsWritesNoWorkflowForYarnOrBun(t *testing.T) {
	for _, lock := range []string{"yarn.lock", "bun.lock"} {
		dir := existingApp(t, map[string]string{lock: "x", "package.json": `{"name":"shop"}`})
		out, _, err := run(t, "agents", "--dir", dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(ciPath))); err == nil || !strings.Contains(out, "note: ") {
			t.Errorf("%s: wrote a workflow or said nothing:\n%s", lock, out)
		}
	}
}
