package symwalk

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Skip("no symbolic links here:", err)
	}
}

// files lists the regular files Walk reports under root, relative and slash-separated.
func files(t *testing.T, root string) []string {
	t.Helper()
	var got []string
	err := Walk(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, path)
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestWalkFollowsLinks(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared")
	write(t, filepath.Join(shared, "logo.svg"), "<svg/>")
	write(t, filepath.Join(shared, "img", "a.png"), "a")
	write(t, filepath.Join(dir, "elsewhere.txt"), "x")
	root := filepath.Join(dir, "public")
	write(t, filepath.Join(root, "plain.txt"), "p")
	link(t, filepath.Join(shared, "logo.svg"), filepath.Join(root, "logo.svg"))
	link(t, filepath.Join(shared, "img"), filepath.Join(root, "img"))
	link(t, filepath.Join("..", "elsewhere.txt"), filepath.Join(root, "rel.txt"))
	want := "img/a.png logo.svg plain.txt rel.txt"
	if got := strings.Join(files(t, root), " "); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// The root itself may be a link.
	alias := filepath.Join(dir, "alias")
	link(t, root, alias)
	if got := strings.Join(files(t, alias), " "); got != want {
		t.Errorf("link as root: got %q, want %q", got, want)
	}
}

func TestWalkStopsAtALoop(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "sub", "a.txt"), "a")
	link(t, root, filepath.Join(root, "sub", "up"))
	err := Walk(root, func(string, fs.DirEntry, error) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "leads back") {
		t.Errorf("a loop: %v", err)
	}
}

func TestWalkReportsABrokenLink(t *testing.T) {
	root := t.TempDir()
	link(t, filepath.Join(root, "gone"), filepath.Join(root, "dangling"))
	var seen error
	Walk(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			seen = err
		}
		return nil
	})
	if !os.IsNotExist(seen) {
		t.Errorf("a broken link: %v", seen)
	}
}

func TestWalkMissingRootIsForFn(t *testing.T) {
	root := filepath.Join(t.TempDir(), "none")
	var path string
	err := Walk(root, func(p string, d fs.DirEntry, err error) error {
		path = p
		return fs.SkipAll
	})
	if err != nil || path != root {
		t.Errorf("%q %v", path, err)
	}
}

func TestWalkSkipDir(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "x.txt"), "x")
	write(t, filepath.Join(root, "b", "y.txt"), "y")
	var got []string
	Walk(root, func(p string, d fs.DirEntry, err error) error {
		if d.IsDir() && d.Name() == "a" {
			return fs.SkipDir
		}
		if d.Type().IsRegular() {
			got = append(got, d.Name())
		}
		return nil
	})
	if strings.Join(got, " ") != "y.txt" {
		t.Errorf("%v", got)
	}
}
