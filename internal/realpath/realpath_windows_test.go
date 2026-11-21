//go:build windows

package realpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// pnpm links each package of node_modules to its folder under .pnpm with a junction, and
// filepath.EvalSymlinks does not follow those since Go 1.23.
func TestResolveFollowsAJunction(t *testing.T) {
	dir, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "store", "pkg")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v\n%s", err, out)
	}
	if got, err := filepath.EvalSymlinks(link); err != nil || got != link {
		t.Logf("filepath.EvalSymlinks(junction) = %q, %v: it follows junctions now, and Resolve may do without the system call", got, err)
	}
	got, err := Resolve(link)
	if err != nil || got != target {
		t.Errorf("Resolve(junction) = %q, %v, want %q", got, err, target)
	}
}

// %TEMP% on a runner is C:\Users\RUNNER~1\..., and Vite names files by their long names.
func TestResolveGivesTheLongName(t *testing.T) {
	long, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	short16 := make([]uint16, windows.MAX_PATH)
	name, err := windows.UTF16PtrFromString(long)
	if err != nil {
		t.Fatal(err)
	}
	n, err := windows.GetShortPathName(name, &short16[0], uint32(len(short16)))
	if err != nil {
		t.Fatal(err)
	}
	short := windows.UTF16ToString(short16[:n])
	got, err := Resolve(short)
	if err != nil || got != long {
		t.Errorf("Resolve(%q) = %q, %v, want %q", short, got, err, long)
	}
}
