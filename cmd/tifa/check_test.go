package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// apiApp copies test/apps/api, an app without pages, into a temporary module that points at this
// checkout, and returns its folder.
func apiApp(t *testing.T) string {
	t.Helper()
	src, dst := filepath.Join("..", "..", "test", "apps", "api"), t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod := filepath.Join(dst, "go.mod")
	data, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "=> ../../..", "=> "+root, 1)
	if edited == string(data) {
		t.Fatal("test/apps/api/go.mod has no replace to the checkout")
	}
	if err := os.WriteFile(gomod, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A good app without pages passes every step, and the TypeScript step says why it is skipped.
func TestCheckPassesAnAppWithoutPages(t *testing.T) {
	dir := apiApp(t)
	out, stderr, err := run(t, "check", "--dir", dir)
	if err != nil {
		t.Fatalf("check: %v\nstdout:\n%s\nstderr:\n%s", err, out, stderr)
	}
	var steps []string
	for _, line := range strings.Split(out, "\n") {
		if step, ok := strings.CutPrefix(line, "==> "); ok {
			steps = append(steps, step)
		}
	}
	want := []string{"analyze", "generate", "go vet ./...", "go test ./...", "typescript: skipped, the app has no pages (no Node needed)"}
	if strings.Join(steps, "|") != strings.Join(want, "|") {
		t.Errorf("steps %q, want %q\n%s", steps, want, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tifa", "main.go")); err != nil {
		t.Errorf("check did not generate .tifa: %v", err)
	}
}

// Each step stops the check at itself: the later steps do not run, the error names the step, and
// what the failing tool printed reaches the user.
func TestCheckStopsAtTheFirstFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    map[string]string
		step     string // the step the error names
		printed  string // what the tool printed, in stdout or stderr
		notReach string // a step that must not start
	}{
		{
			name:     "vet",
			files:    map[string]string{"vetbad.go": "package apitest\n\nimport \"fmt\"\n\nfunc vetBad() { fmt.Printf(\"%d\\n\", \"text\") }\n"},
			step:     "go vet ./...",
			printed:  "vetbad.go:5:",
			notReach: "==> go test",
		},
		{
			name:     "test",
			files:    map[string]string{"boom_test.go": "package apitest\n\nimport \"testing\"\n\nfunc TestBoom(t *testing.T) { t.Fatal(\"boom\") }\n"},
			step:     "go test ./...",
			printed:  "boom",
			notReach: "==> typescript",
		},
		{
			name:     "go type error",
			files:    map[string]string{"broken.go": "package apitest\n\nvar _ int = \"text\"\n"},
			step:     "analyze",
			printed:  "broken.go:3:",
			notReach: "==> generate",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := apiApp(t)
			for name, content := range tc.files {
				writeFile(t, filepath.Join(dir, name), content)
			}
			out, stderr, err := run(t, "check", "--dir", dir)
			if err == nil {
				t.Fatalf("check passed:\n%s", out)
			}
			if want := "check failed at " + tc.step + ": "; !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not say %q", err, want)
			}
			if !strings.Contains(out+stderr, tc.printed) {
				t.Errorf("the output has no %q:\nstdout:\n%s\nstderr:\n%s", tc.printed, out, stderr)
			}
			if strings.Contains(out, tc.notReach) {
				t.Errorf("a step after the failure started:\n%s", out)
			}
		})
	}
}

// A convention violation stops the check before anything is generated or run, with the file and line.
func TestCheckStopsAtConventionViolations(t *testing.T) {
	dir := filepath.Join("..", "..", "internal", "analyze", "testdata", "duppath")
	out, stderr, err := run(t, "check", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "check failed at analyze: ") {
		t.Fatalf("check: %v\n%s", err, out)
	}
	if !strings.Contains(stderr, "app/items/key+/route.go:1:") {
		t.Errorf("stderr does not name the violation:\n%s", stderr)
	}
	if strings.Contains(out, "==> generate") {
		t.Errorf("generation started after the violation:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tifa")); err == nil {
		t.Error("a failed analysis generated .tifa")
	}
}

// The type check runs the TypeScript the app installs, found the way Node finds it, and tells an
// app that lacks it what to run.
func TestCheckNeedsTheAppsTypeScript(t *testing.T) {
	err := typeCheck(t.Context(), t.TempDir(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "typescript is not installed, run the install of your package manager in the app root") {
		t.Errorf("typeCheck without node_modules: %v", err)
	}
}
