package devserver

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// The watcher follows random changes to a tree: files and directories created, removed, renamed
// and saved atomically, files that become directories and back, and links created, pointed
// elsewhere and removed, some to targets outside the tree that go away and come back. After each
// step, once the watcher is quiet, its snapshot matches the disk, and so do the changes it
// reported. TIFA_STRESS_STEPS sets the number of steps per seed.
func TestWatcherFollowsRandomChanges(t *testing.T) {
	steps := 40
	if s := os.Getenv("TIFA_STRESS_STEPS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatal(err)
		}
		steps = n
	}
	for _, c := range []struct {
		seed   int64
		budget int // 0 keeps the system's
	}{{1, 0}, {2, 0}, {3, 0}, {4, 12}, {5, 6}} {
		t.Run(fmt.Sprintf("seed %d budget %d", c.seed, c.budget), func(t *testing.T) {
			stress(t, c.seed, c.budget, steps)
		})
	}
}

func stress(t *testing.T, seed int64, budget, steps int) {
	root, ext := t.TempDir(), t.TempDir()
	r := rand.New(rand.NewSource(seed))
	w := preparedWatcher(t, root, func(string) {})
	if budget > 0 {
		w.budget = budget
	}
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{} // the tracked files as the reported changes tell
	for i := range steps {
		did := randomStep(r, root, ext)
		settle(t, w)
		for _, c := range w.changes.take() {
			if c.op == removed {
				delete(seen, c.path)
			} else {
				seen[c.path] = true
			}
		}
		if err := checkSnapshot(w, seen); err != nil {
			t.Fatalf("step %d, %s: %v", i, did, err)
		}
	}
}

// settle hands the watcher's events to it, and lists and polls as run does, until it is quiet.
func settle(t *testing.T, w *watcher) {
	t.Helper()
	for quiet := 0; quiet < 3; {
		select {
		case ev := <-w.events:
			if err := w.recover(w.handle(ev.Name, ev.Op)); err != nil {
				t.Fatal(err)
			}
			quiet = 0
		case err := <-w.errs:
			t.Fatal(err)
		case <-time.After(20 * time.Millisecond):
			for _, step := range []func() error{w.listDirty, w.checkLinks, w.poll} {
				if err := w.recover(step()); err != nil {
					t.Fatal(err)
				}
			}
			if len(w.dirty) == 0 {
				quiet++
			}
		}
	}
}

var (
	stressDirs  = []string{"a", "b", "c"}
	stressFiles = []string{"x.ts", "y.ts", "s.css", "p.png"}
	stressLinks = []string{"l1", "l2", "m.ts"}
)

// randomStep changes something under root or ext and says what.
func randomStep(r *rand.Rand, root, ext string) string {
	pick := func(list []string) string { return list[r.Intn(len(list))] }
	base := func() string {
		if r.Intn(4) == 0 {
			return ext
		}
		return root
	}
	b := base()
	dir := pick(entries(b, true))
	at := filepath.Join(b, dir)
	content := fmt.Sprintf("v%d", r.Intn(1000))
	any := func() string { return filepath.Join(b, pick(entries(b, false))) }
	switch op := r.Intn(11); op {
	case 0: // write a file
		p := filepath.Join(at, pick(stressFiles))
		os.WriteFile(p, []byte(content), 0o644)
		return "write " + p
	case 1: // save a file atomically
		p := filepath.Join(at, pick(stressFiles))
		os.WriteFile(p+".save", []byte(content), 0o644)
		os.Rename(p+".save", p)
		return "save " + p
	case 2: // remove anything
		p := any()
		os.RemoveAll(p)
		return "remove " + p
	case 3: // rename anything
		p, q := any(), filepath.Join(at, pick(append(slices.Clone(stressDirs), stressFiles...)))
		os.Rename(p, q)
		return "rename " + p + " to " + q
	case 4: // make a directory with a file in it
		p := filepath.Join(at, pick(stressDirs))
		os.Mkdir(p, 0o755)
		os.WriteFile(filepath.Join(p, pick(stressFiles)), []byte(content), 0o644)
		return "mkdir " + p
	case 5: // a file becomes a directory, or a directory a file
		p := filepath.Join(at, pick(stressFiles))
		if info, err := os.Lstat(p); err == nil && info.IsDir() {
			os.RemoveAll(p)
			os.WriteFile(p, []byte(content), 0o644)
			return "directory to file " + p
		}
		os.RemoveAll(p)
		os.Mkdir(p, 0o755)
		os.WriteFile(filepath.Join(p, pick(stressFiles)), []byte(content), 0o644)
		return "file to directory " + p
	case 6, 7: // make a link, or point one elsewhere
		p := filepath.Join(at, pick(stressLinks))
		target := filepath.Join(pick([]string{root, ext}), pick(entries(base(), false)))
		if r.Intn(5) == 0 {
			target = filepath.Join(ext, "missing", pick(stressFiles))
		}
		os.Symlink(target, p+".new")
		os.Rename(p+".new", p)
		return "link " + p + " to " + target
	case 8: // remove a link
		p := filepath.Join(at, pick(stressLinks))
		os.Remove(p)
		return "unlink " + p
	case 9: // a target outside goes away and comes back
		p := filepath.Join(ext, pick(entries(ext, false)))
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&fs.ModeSymlink != 0 {
			return "nothing"
		}
		data, _ := os.ReadFile(p)
		os.RemoveAll(p)
		time.Sleep(time.Duration(r.Intn(3)) * 20 * time.Millisecond)
		if info.IsDir() {
			os.Mkdir(p, 0o755)
			os.WriteFile(filepath.Join(p, pick(stressFiles)), []byte(content), 0o644)
		} else {
			os.WriteFile(p, data, 0o644)
		}
		return "remove and restore " + p
	default: // make the missing target of a link
		p := filepath.Join(ext, "missing")
		os.MkdirAll(p, 0o755)
		os.WriteFile(filepath.Join(p, pick(stressFiles)), []byte(content), 0o644)
		return "make " + p
	}
}

// entries lists what is under base, relative to it, without following links: only directories
// when dirs is set, and then base itself as ".".
func entries(base string, dirs bool) []string {
	var list []string
	if dirs {
		list = append(list, ".")
	}
	filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == base {
			return nil
		}
		if rel, err := filepath.Rel(base, p); err == nil && (!dirs || d.IsDir()) {
			list = append(list, rel)
		}
		return nil
	})
	if len(list) == 0 {
		list = append(list, "none")
	}
	return list
}

// checkSnapshot compares the watcher's snapshot with the disk: each directory node is the
// directory at its path, every entry of it has the node it should have, and the files the
// reported changes tell of are the file nodes.
func checkSnapshot(w *watcher, seen map[string]bool) error {
	files := map[string]bool{}
	for rel, n := range w.nodes {
		if rel != "." && !w.isDir(path.Dir(rel)) {
			return fmt.Errorf("%s is in the snapshot, its directory is not", rel)
		}
		if n.kind == fileNode {
			files[rel] = true
		}
		if n.kind != dirNode {
			continue
		}
		info, err := os.Stat(w.abs(rel))
		if err != nil || !os.SameFile(info, n.info) {
			return fmt.Errorf("directory node %s is not the directory there: %v", rel, err)
		}
		if real, err := filepath.EvalSymlinks(w.abs(rel)); err != nil || real != n.real || w.owner[real] != rel {
			return fmt.Errorf("directory node %s: real path %s, on disk %s (%v), owner %s", rel, n.real, real, err, w.owner[real])
		}
		if !w.polled[rel] && !w.watches[n.real] {
			return fmt.Errorf("directory node %s is neither watched nor polled", rel)
		}
		if err := checkEntries(w, rel); err != nil {
			return err
		}
	}
	for rel := range seen {
		if !files[rel] {
			return fmt.Errorf("the changes tell of %s, the snapshot has no file there", rel)
		}
	}
	for rel := range files {
		if !seen[rel] {
			return fmt.Errorf("the snapshot has the file %s, the changes do not tell of it", rel)
		}
	}
	if w.watchCount() > w.budget {
		return fmt.Errorf("%d watches for a budget of %d", w.watchCount(), w.budget)
	}
	return nil
}

// checkEntries compares the entries of the directory node dir with the disk.
func checkEntries(w *watcher, dir string) error {
	list, err := os.ReadDir(w.abs(dir))
	if err != nil {
		return fmt.Errorf("directory node %s: %v", dir, err)
	}
	present := map[string]bool{}
	for _, e := range list {
		rel := path.Join(dir, e.Name())
		if w.skipped(rel) {
			continue
		}
		present[rel] = true
		n := w.nodes[rel]
		li, err := os.Lstat(w.abs(rel))
		if err != nil {
			return fmt.Errorf("%s: %v", rel, err)
		}
		link := li.Mode()&fs.ModeSymlink != 0
		info, err := os.Stat(w.abs(rel))
		switch {
		case err != nil:
			if !link || n == nil || n.kind != linkNode {
				return fmt.Errorf("%s leads nowhere (%v), its node is %+v", rel, err, n)
			}
		case info.IsDir():
			real, _ := filepath.EvalSymlinks(w.abs(rel))
			owner, owned := w.owner[real]
			switch {
			case n == nil || n.kind != dirNode && n.kind != aliasNode:
				return fmt.Errorf("directory %s has the node %+v", rel, n)
			case n.real != real || !owned:
				return fmt.Errorf("directory %s: node for %s, on disk %s, owned by %q", rel, n.real, real, owner)
			case n.kind == aliasNode && owner == rel:
				return fmt.Errorf("directory %s is an alias of itself", rel)
			}
		case info.Mode().IsRegular() && kindOf(rel) != other:
			sum, err := checksum(w.abs(rel))
			if err != nil {
				return fmt.Errorf("%s: %v", rel, err)
			}
			if n == nil || n.kind != fileNode || n.sum != sum {
				return fmt.Errorf("file %s has the node %+v, its checksum is %s", rel, n, sum)
			}
			if watchesFiles && w.sys != nil && !w.polled[dir] && n.watch == "" {
				return fmt.Errorf("file %s in a watched directory has no watch", rel)
			}
		default:
			if link && (n == nil || n.kind != linkNode) || !link && n != nil {
				return fmt.Errorf("%s, a file nobody cares about, has the node %+v", rel, n)
			}
		}
	}
	for kid := range w.kids[dir] {
		if !present[kid] {
			return fmt.Errorf("%s is in the snapshot, not on disk", kid)
		}
	}
	return nil
}
