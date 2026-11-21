package realpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFollowsASymbolicLink(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}
	got, err := Resolve(link)
	if err != nil || got != target {
		t.Errorf("Resolve(link) = %q, %v, want %q", got, err, target)
	}
}

func TestResolveOfAMissingPathFails(t *testing.T) {
	if got, err := Resolve(filepath.Join(t.TempDir(), "nothing")); err == nil {
		t.Errorf("Resolve of a missing path = %q, want an error", got)
	}
}
