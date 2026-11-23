package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// autoSetup runs setup for an app whose tifa.yaml has the deploy block deploy, with the files given,
// against a repository whose default branch is main. It returns the output, the error and the root.
func autoSetup(t *testing.T, deploy string, files map[string]string, extra ...string) (string, error, string, *fakeGH) {
	t.Helper()
	root := setupApp(t, "deploy:\n"+deploy, files)
	newSetupServer(t, "hostname 203.0.113.7\nport 2222\n", factsOf("shop", 9531, ""))
	gh := newFakeGH()
	gh.install(t)
	out, err := runSetup(t, root, false, "", append([]string{"--yes"}, extra...)...)
	return out, err, root, gh
}

func workflowOn(t *testing.T, root string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(root, githubWorkflowPath))
	if err != nil {
		t.Fatal(err)
	}
	on, _, _ := strings.Cut(string(text), "\n# One deploy at a time")
	_, on, _ = strings.Cut(on, "\non:\n")
	return on
}

// The defaults: the default branch of the repository, after ci.yml passed.
func TestSetupAutoDefaults(t *testing.T) {
	out, err, root, _ := autoSetup(t, "  auto: {}\n", nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	on := workflowOn(t, root)
	for _, want := range []string{"workflow_run:", `workflows: ["CI"]`, "types: [completed]", "branches: [main]", "workflow_dispatch:"} {
		if !strings.Contains(on, want) {
			t.Errorf("lacks %q:\n%s", want, on)
		}
	}
	if strings.Contains(on, "push:") || strings.Contains(out, "default branch") {
		t.Errorf("on:\n%s\nplan:\n%s", on, out)
	}
}

// A branch that is not the default one: the workflow listens to it, and the plan says the file must
// be on the default branch, which is the only one GitHub starts a workflow_run workflow from.
func TestSetupAutoOtherBranch(t *testing.T) {
	ci := "name: Checks\non:\n  push:\n    branches: [main, release]\n"
	out, err, root, _ := autoSetup(t, "  auto:\n    branch: release\n    needs: checks.yml\n", map[string]string{".github/workflows/checks.yml": ci})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	on := workflowOn(t, root)
	for _, want := range []string{"workflow_run:", `workflows: ["Checks"]`, "branches: [release]"} {
		if !strings.Contains(on, want) {
			t.Errorf("lacks %q:\n%s", want, on)
		}
	}
	for _, want := range []string{"which you commit and push to main, the default branch", "only from the file there"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
}

// needs: none waits for nothing, so the workflow runs on a push to the branch.
func TestSetupAutoNeedsNone(t *testing.T) {
	out, err, root, _ := autoSetup(t, "  auto:\n    needs: none\n    branch: release\n", nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	on := workflowOn(t, root)
	if !strings.Contains(on, "push:\n    branches: [release]") || strings.Contains(on, "workflow_run") {
		t.Errorf("on:\n%s", on)
	}
	if strings.Contains(out, "default branch") {
		t.Errorf("a push workflow runs from the branch itself:\n%s", out)
	}
}

// A workflow to wait for that is missing, has no name or does not run on a push to the branch is
// something for the developer: setup writes no workflow and exits with an error. It never deploys
// without waiting.
func TestSetupAutoNeedsProblems(t *testing.T) {
	for name, tc := range map[string]struct {
		deploy string
		files  map[string]string
		why    string
	}{
		"missing": {"  auto:\n    needs: checks.yml\n", nil, ".github/workflows/checks.yml does not exist"},
		"no name": {"  auto: {}\n", map[string]string{testCIPath: "on: push\n"}, "has no name: line"},
		"not on push to the branch": {"  auto:\n    branch: release\n", nil,
			".github/workflows/ci.yml does not run on a push to release (its push branches are main)"},
		"pull request only": {"  auto: {}\n", map[string]string{testCIPath: "name: CI\non: pull_request\n"}, "does not run on a push to main (its on: has no push)"},
	} {
		out, err, root, _ := autoSetup(t, tc.deploy, tc.files)
		if err == nil || !strings.Contains(err.Error(), "needs you") {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
		if _, statErr := os.Stat(filepath.Join(root, githubWorkflowPath)); statErr == nil {
			t.Errorf("%s: a workflow was written", name)
		}
		for _, want := range []string{"github: deploys from GitHub Actions on ", tc.why, "write needs: none"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: lacks %q:\n%s", name, want, out)
			}
		}
	}
}

// The subject of the Tailscale identity is the ref the deploy job runs with: a workflow_run job runs
// with the default branch, and a push job with the branch that was pushed.
func TestSetupAutoTailscaleSubject(t *testing.T) {
	for name, tc := range map[string]struct{ auto, want string }{
		"default":                 {"  auto: {}\n", "repo:acme/shop:ref:refs/heads/main (GitHub gives the run that starts after ci.yml passed (workflow_run) the default branch"},
		"workflow_run of release": {"  auto:\n    branch: release\n", "repo:acme/shop:ref:refs/heads/main (GitHub gives the run that starts after ci.yml passed"},
		"push of release":         {"  auto:\n    branch: release\n    needs: none\n", "repo:acme/shop:ref:refs/heads/release (GitHub gives a run started by a push to release that ref"},
	} {
		files := map[string]string{testCIPath: "name: CI\non: push\n"}
		out, _, _, _ := autoSetup(t, "  connect: tailscale\n"+tc.auto, files)
		if !strings.Contains(out, tc.want) {
			t.Errorf("%s: lacks %q:\n%s", name, tc.want, out)
		}
	}
}

// Two workflows can share a name, so the deploy job also checks the file of the run that finished.
func TestSetupAutoBindsNeedsToTheFile(t *testing.T) {
	out, err, root, _ := autoSetup(t, "  auto:\n    needs: checks.yml\n", map[string]string{".github/workflows/checks.yml": "name: Checks\non: push\n"})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	text, _ := os.ReadFile(filepath.Join(root, githubWorkflowPath))
	want := "(github.event.workflow_run.path == '.github/workflows/checks.yml' || startsWith(github.event.workflow_run.path, '.github/workflows/checks.yml@'))"
	if !strings.Contains(string(text), want) {
		t.Errorf("the job's if lacks the file:\n%s", text)
	}
	_, err, root, _ = autoSetup(t, "  auto:\n    needs: none\n", nil)
	if text, _ := os.ReadFile(filepath.Join(root, githubWorkflowPath)); err != nil || strings.Contains(string(text), "workflow_run.path") {
		t.Errorf("needs none has no workflow_run: %v\n%s", err, text)
	}
}
