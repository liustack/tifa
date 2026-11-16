package bundle

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pin hard-links path to a name in a folder of its own, so that the file keeps its inode while the pin lives:
// a later build that writes the file again gives it another one, whatever the file system reuses.
func pin(t *testing.T, path string) os.FileInfo {
	t.Helper()
	pinned := filepath.Join(t.TempDir(), "pin")
	if err := os.Link(path, pinned); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(pinned)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A rebuild of tifa dev leaves the files in dist/public alone when public/ did not change, brings
// the ones that changed up to date, and drops the ones that went. A request for a public file
// never meets a gap while it does.
func TestDevRebuildSyncsPublicInPlace(t *testing.T) {
	root := devApp(t)
	public := filepath.Join(root, "public")
	writeTree(t, root, map[string]string{
		"public/keep.txt":       "keep",
		"public/change.txt":     "old",
		"public/gone.txt":       "gone",
		"public/img/inner.txt":  "inner",
		"public/dir/only.txt":   "only",
		"outside/linked.txt":    "linked",
		"outside/folder/in.txt": "in folder",
	})
	if err := os.Symlink(filepath.Join(root, "outside", "linked.txt"), filepath.Join(public, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside", "folder"), filepath.Join(public, "folder")); err != nil {
		t.Fatal(err)
	}
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	build := func() {
		t.Helper()
		if _, err := d.Build(context.Background(), public, true); err != nil {
			t.Fatalf("Build: %v", err)
		}
	}
	out := filepath.Join(root, ".tifa", "dist", "public")
	build()
	for name, want := range map[string]string{
		"keep.txt": "keep", "link.txt": "linked", "folder/in.txt": "in folder", "img/inner.txt": "inner",
	} {
		if got := readFile(t, filepath.Join(out, name)); got != want {
			t.Fatalf("first build: %s holds %q, want %q", name, got, want)
		}
	}

	keep := pin(t, filepath.Join(out, "keep.txt"))
	link := pin(t, filepath.Join(out, "link.txt"))
	build()
	for name, pinned := range map[string]os.FileInfo{"keep.txt": keep, "link.txt": link} {
		now, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(now, pinned) {
			t.Errorf("%s was written again by a rebuild with public/ unchanged", name)
		}
	}

	// A change of a file, of a linked file, an added file, a removed file and a removed folder.
	later := time.Now().Add(time.Hour)
	writeTree(t, root, map[string]string{
		"public/change.txt":   "new",
		"outside/linked.txt":  "linked again",
		"public/added.txt":    "added",
		"public/new/deep.txt": "deep",
	})
	for _, f := range []string{"public/change.txt", "outside/linked.txt"} {
		if err := os.Chtimes(filepath.Join(root, f), later, later); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"public/gone.txt", "public/dir"} {
		if err := os.RemoveAll(filepath.Join(root, f)); err != nil {
			t.Fatal(err)
		}
	}
	build()
	for name, want := range map[string]string{
		"change.txt": "new", "link.txt": "linked again", "added.txt": "added", "new/deep.txt": "deep", "keep.txt": "keep",
	} {
		if got := readFile(t, filepath.Join(out, name)); got != want {
			t.Errorf("%s holds %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"gone.txt", "dir"} {
		if _, err := os.Stat(filepath.Join(out, name)); !os.IsNotExist(err) {
			t.Errorf("%s stays in dist/public after it left public/: %v", name, err)
		}
	}
	now, err := os.Stat(filepath.Join(out, "keep.txt"))
	if err != nil || !os.SameFile(now, keep) {
		t.Errorf("keep.txt was written again by a rebuild that changed other files: %v", err)
	}

	// A file that becomes a folder and a folder that becomes a file.
	if err := os.Remove(filepath.Join(public, "added.txt")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"public/added.txt/sub.txt": "sub"})
	if err := os.RemoveAll(filepath.Join(public, "new")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"public/new": "plain"})
	build()
	if got := readFile(t, filepath.Join(out, "added.txt", "sub.txt")); got != "sub" {
		t.Errorf("added.txt/sub.txt holds %q", got)
	}
	if got := readFile(t, filepath.Join(out, "new")); got != "plain" {
		t.Errorf("new holds %q", got)
	}

	// public/ gone: dist/public holds no file. The first build above had it, a missing one is the
	// case of an app without public files.
	if err := os.RemoveAll(public); err != nil {
		t.Fatal(err)
	}
	build()
	if entries, _ := os.ReadDir(out); len(entries) > 0 {
		t.Errorf("dist/public holds %d entries after public/ went", len(entries))
	}
}

// With no public/ at all, the first build works and leaves no file in dist/public.
func TestDevBuildWithoutPublic(t *testing.T) {
	root := devApp(t)
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
}

// A symlinked public/ is followed, as in tifa build.
func TestDevBuildFollowsALinkedPublic(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{"elsewhere/a.txt": "a"})
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(root, "public")); err != nil {
		t.Fatal(err)
	}
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := readFile(t, filepath.Join(root, ".tifa", "dist", "public", "a.txt")); got != "a" {
		t.Errorf("a.txt holds %q", got)
	}
}
