package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNewApp creates an app with tifa new, follows the steps it prints, and checks that the app
// tests, builds and answers.
func TestNewApp(t *testing.T) {
	skipUnlessEnabled(t)
	modules := filepath.Join(repoRoot(t), "test", "apps", "pages", "node_modules")
	if _, err := os.Stat(modules); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}
	dir := filepath.Join(t.TempDir(), "hello")
	out, err := exec.Command(cli(t), "new", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("tifa new: %v\n%s", err, out)
	}
	// Follow the printed steps in their order. The CLI is built in this checkout, so the steps
	// point go.mod at it. pnpm install is replaced by the link below, and tifa dev by the build.
	_, next, found := strings.Cut(string(out), "\nNext:\n")
	if !found {
		t.Fatalf("tifa new printed no next steps:\n%s", out)
	}
	var ran []string
	for _, line := range strings.Split(strings.TrimSpace(next), "\n") {
		step, _, _ := strings.Cut(line, "#")
		step = strings.TrimSpace(step)
		ran = append(ran, step)
		switch {
		case strings.HasPrefix(step, "cd "):
			if step != "cd "+dir {
				t.Fatalf("step %q does not enter %s", step, dir)
			}
		case step == "pnpm install", step == "go tool tifa dev":
		default:
			step = strings.ReplaceAll(step, "<path to your tifa checkout>", repoRoot(t))
			args := strings.Fields(step)
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v\n%s", step, err, out)
			}
		}
	}
	want := []string{"cd " + dir, "go mod edit -replace github.com/liustack/tifa=<path to your tifa checkout>", "go mod tidy", "pnpm install", "go tool tifa dev"}
	if strings.Join(ran, "\n") != strings.Join(want, "\n") {
		t.Fatalf("steps %q, want %q", ran, want)
	}
	for _, step := range [][]string{{"go", "vet", "./..."}, {"go", "test", "./..."}} {
		cmd := exec.Command(step[0], step[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(step, " "), err, out)
		}
	}
	// The app installs the packages of test/apps/pages, which TestNewAppUsesTheTestedPackages in
	// cmd/tifa checks.
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	base, _, output := start(t, buildAt(t, dir))
	clicks := func(n string) *regexp.Regexp {
		return regexp.MustCompile(`<p>Presses: (<!-- -->)?` + n + `</p>`)
	}
	if status, _, body := get(t, base+"/"); status != 200 || !clicks("0").MatchString(body) || !strings.Contains(body, "<title>hello</title>") {
		t.Fatalf("GET /: %d\n%s\n%s", status, body, output)
	}
	if status, _, body := request(t, "POST", base+"/", ""); status != 200 || strings.TrimSpace(body) != `{"clicks":1}` {
		t.Fatalf("POST /: %d %s", status, body)
	}
	if status, _, body := get(t, base+"/"); status != 200 || !clicks("1").MatchString(body) {
		t.Fatalf("GET / after POST: %d\n%s", status, body)
	}
	if status, _, _ := get(t, base+"/nowhere"); status != 404 {
		t.Fatalf("GET /nowhere: %d", status)
	}
	typecheck(t, dir)
}
