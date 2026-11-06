// Package langs reads the languages of an app from its _locales folders. The analysis for
// tifa build and tifa dev, and tifa.Languages in a program that tifa build did not make (go test,
// go run), read them the same way.
package langs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/config"
)

// Dir is the name of the folder of an area that holds its <language>.json files.
const Dir = "_locales"

// Order puts def first and the other languages after it in byte order. It leaves langs alone.
func Order(langs []string, def string) []string {
	out := slices.Clone(langs)
	slices.Sort(out)
	if i := slices.Index(out, def); i > 0 {
		out = slices.Concat([]string{def}, out[:i], out[i+1:])
	}
	return out
}

// Read lists the languages of the _locales folder at dir, in byte order: the names of its
// <language>.json files. <language>.draft.json and files of another kind do not count. A .json
// file whose name is no language tag, or not in its canonical spelling, goes to invalid with the
// reason.
func Read(dir string) (langs []string, invalid map[string]error, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || strings.HasSuffix(name, ".draft") {
			continue
		}
		if err := config.CheckLanguage(name); err != nil {
			if invalid == nil {
				invalid = map[string]error{}
			}
			invalid[e.Name()] = err
			continue
		}
		langs = append(langs, name)
	}
	return langs, invalid, nil
}

// Module returns the root of the module that holds dir: the nearest folder at or above it with a
// go.mod, "" when there is none.
func Module(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Site returns the languages of the site of the app rooted at root: the languages of every
// _locales folder below app/, the default language of tifa.yaml first and the others in byte
// order. It is nil for an app without one. It reads the files only and checks nothing, which tifa
// build does, so a folder it cannot read is left out.
func Site(root string) []string {
	set := map[string]bool{}
	_ = filepath.WalkDir(filepath.Join(root, "app"), func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case !d.IsDir():
			return nil
		case d.Name() == Dir:
			if langs, _, err := Read(path); err == nil {
				for _, l := range langs {
					set[l] = true
				}
			}
			return filepath.SkipDir
		case path != filepath.Join(root, "app") && SkippedName(d.Name()):
			return filepath.SkipDir
		}
		return nil
	})
	if len(set) == 0 {
		return nil
	}
	var all []string
	for l := range set {
		all = append(all, l)
	}
	def := ""
	if data, err := os.ReadFile(filepath.Join(root, "tifa.yaml")); err == nil || errors.Is(err, fs.ErrNotExist) {
		if cfg, _, err := config.Project(data); err == nil {
			def = cfg.I18n.Default
		}
	}
	return Order(all, def)
}

// SkippedName reports whether a folder of app/ with this name holds no area: the go command skips
// folders that start with . or _ (the _locales folder itself is looked at before this), and
// node_modules is not the app's. tifa build and tifa.Languages both use it, so they find the
// same _locales folders.
func SkippedName(name string) bool {
	return name == "node_modules" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// Hidden reports whether any folder in the path rel, the folders between app/ and a _locales
// folder with forward slashes, is skipped.
func Hidden(rel string) bool {
	for _, name := range strings.Split(rel, "/") {
		if name != "" && name != "." && SkippedName(name) {
			return true
		}
	}
	return false
}
