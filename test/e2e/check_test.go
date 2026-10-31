package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tifaCheck runs tifa check on the app in dir and returns what it printed in both streams.
func tifaCheck(t *testing.T, dir string) (string, error) {
	t.Helper()
	out, err := exec.Command(cli(t), "check", "--dir", dir).CombinedOutput()
	return string(out), err
}

// tifa check passes on the example shop, which has pages, and then stops at the first failing
// step with a message that names it: the TypeScript of a page, then the Go vet of the app.
func TestCheckShop(t *testing.T) {
	skipUnlessEnabled(t)
	if _, err := os.Stat(filepath.Join(shopDir(t), "node_modules")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in examples/shop first")
	}
	dir := copyShop(t)
	out, err := tifaCheck(t, dir)
	if err != nil {
		t.Fatalf("tifa check on the shop: %v\n%s", err, out)
	}
	for _, want := range []string{"==> analyze", "==> generate", "==> go vet ./...", "==> go test ./...", "==> typescript (tsc --noEmit)", "check passed"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output has no %q:\n%s", want, out)
		}
	}

	format := filepath.Join(dir, "app", "format.ts")
	original, err := os.ReadFile(format)
	if err != nil {
		t.Fatal(err)
	}
	write(t, format, string(original)+"\nexport const notANumber: number = \"text\"\n")
	out, err = tifaCheck(t, dir)
	if err == nil || !strings.Contains(out, "check failed at typescript (tsc --noEmit)") || !strings.Contains(out, "app/format.ts") || !strings.Contains(out, "TS2322") {
		t.Fatalf("a type error in app/format.ts: %v\n%s", err, out)
	}
	if strings.Contains(out, "check passed") {
		t.Errorf("a failed check says it passed:\n%s", out)
	}

	write(t, format, string(original))
	write(t, filepath.Join(dir, "vetbad.go"), "package shop\n\nimport \"fmt\"\n\nfunc vetBad() { fmt.Printf(\"%d\\n\", \"text\") }\n")
	out, err = tifaCheck(t, dir)
	if err == nil || !strings.Contains(out, "check failed at go vet ./...") || !strings.Contains(out, "vetbad.go:5:") {
		t.Fatalf("a vet error: %v\n%s", err, out)
	}
	if strings.Contains(out, "==> go test") || strings.Contains(out, "==> typescript") {
		t.Errorf("a step after the failing one ran:\n%s", out)
	}
}

// An app without pages needs no Node: tifa check runs with node removed from PATH and skips the
// TypeScript step.
func TestCheckAppWithoutPages(t *testing.T) {
	skipUnlessEnabled(t)
	cmd := exec.Command(cli(t), "check", "--dir", filepath.Join(repoRoot(t), "test", "apps", "api"))
	cmd.Env = append(os.Environ(), "PATH="+withoutNode(t))
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "==> typescript: skipped, the app has no pages") {
		t.Fatalf("tifa check on test/apps/api: %v\n%s", err, out)
	}
}
