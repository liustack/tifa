package analyze

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Embedded is what the go:embed lines of the packages of the module bring into the program. A
// change to one of these files changes the program, so tifa dev builds it again. The paths are
// relative to the application root and use forward slashes.
type Embedded struct {
	root     string          // the application root on disk, to find the folders that are other modules
	files    map[string]bool // the files the go command found
	patterns []embedPattern  // the go:embed patterns, which also take files that do not exist yet
}

// embedPattern is one pattern of a go:embed line, written in the package in dir.
type embedPattern struct {
	dir     string // directory of the package, "." for the root
	pattern string // as written, without the all: prefix
	all     bool   // all: keeps names that start with . or _ below a folder
}

// Has reports whether the program embeds the file rel, or would embed it once it exists. The
// go command takes a file a pattern names, and the files below a folder a pattern names except
// those whose name starts with . or _ (all: keeps them) and those in a folder that holds a
// go.mod, which is another module. A nil Embedded embeds nothing.
func (e *Embedded) Has(rel string) bool {
	if e == nil {
		return false
	}
	if e.files[rel] {
		return true
	}
	for _, p := range e.patterns {
		if p.matches(e.root, rel) {
			return true
		}
	}
	return false
}

// matches reports whether the pattern takes the file rel of the application at root.
func (p embedPattern) matches(root, rel string) bool {
	full := strings.Split(rel, "/")
	if p.dir != "." {
		var ok bool
		if rel, ok = strings.CutPrefix(rel, p.dir+"/"); !ok {
			return false
		}
	}
	elems := strings.Split(rel, "/")
	n := strings.Count(p.pattern, "/") + 1
	if len(elems) < n {
		return false
	}
	if ok, err := path.Match(p.pattern, strings.Join(elems[:n], "/")); err != nil || !ok {
		return false
	}
	if len(elems) == n {
		return true
	}
	// Below the folder the pattern names, every name on the way must be one the go command takes.
	below := elems[n:]
	for _, name := range below {
		if !p.all && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
			return false
		}
	}
	// A folder with a go.mod on the way is another module, which the go command leaves out. The
	// disk says so even for a file that is not there yet.
	named := len(full) - len(below) // the elements up to the folder the pattern names
	for k := named + 1; k < len(full); k++ {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(strings.Join(full[:k], "/")), "go.mod")); err == nil {
			return false
		}
	}
	return true
}

// embedded collects the embedded files and the patterns of the packages that go list loaded.
func (a *analyzer) embedded(pkgs []*packages.Package) *Embedded {
	e := &Embedded{root: a.root, files: map[string]bool{}}
	rel := func(abs string) (string, bool) {
		r, err := filepath.Rel(a.root, abs)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.ToSlash(r), true
	}
	for _, p := range pkgs {
		if len(p.GoFiles) == 0 {
			continue
		}
		pkgDir := filepath.Dir(p.GoFiles[0])
		dir, ok := rel(pkgDir)
		if !ok {
			continue
		}
		for _, f := range p.EmbedFiles {
			if r, ok := rel(f); ok {
				e.files[r] = true
			}
		}
		for _, pattern := range p.EmbedPatterns {
			// go/packages joins each pattern to the directory of the package, "all:" and all.
			r, ok := strings.CutPrefix(pattern, pkgDir+string(filepath.Separator))
			if !ok {
				continue
			}
			r, all := strings.CutPrefix(filepath.ToSlash(r), "all:")
			e.patterns = append(e.patterns, embedPattern{dir: dir, pattern: r, all: all})
		}
	}
	return e
}
