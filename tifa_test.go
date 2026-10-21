package tifa_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A separate module must compile the public API without access to internal packages.
func TestExternalAppHandlesSSRBusy(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := fmt.Sprintf("module example.com/overloadapp\n\ngo 1.27.0\n\nrequire github.com/liustack/tifa v0.0.0\nreplace github.com/liustack/tifa => %q\n", root)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{"go.sum": "go.sum", "hooks_test.go": "testdata/overloadapp/hooks_test.go"} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-p", "2", "-pgo=off", "-mod=mod", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external app: %v\n%s", err, out)
	}
}
