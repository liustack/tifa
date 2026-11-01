package analyze

import (
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Embedded is what the go:embed lines of the packages of the module bring into the program. A
// change to one of these files changes the program, so tifa dev builds it again. The paths are
// relative to the application root and use forward slashes.
type Embedded struct {
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
// those whose name starts with . or _ (all: keeps them). A nil Embedded embeds nothing.
func (e *Embedded) Has(rel string) bool {
	if e == nil {
		return false
	}
	if e.files[rel] {
		return true
	}
	for _, p := range e.patterns {
		if p.matches(rel) {
			return true
		}
	}
	return false
}

func (p embedPattern) matches(rel string) bool {
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
	if len(elems) == n || p.all {
		return true
	}
	for _, name := range elems[n:] {
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			return false
		}
	}
	return true
}

// embedded collects the embedded files and the patterns of the packages that go list loaded.
func (a *analyzer) embedded(pkgs []*packages.Package) *Embedded {
	e := &Embedded{files: map[string]bool{}}
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
