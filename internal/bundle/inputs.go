package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/liustack/tifa/internal/langs"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// InputsFile is the file of dist that records what a build was made from: the hash of each file it
// read, by its path relative to the application root with forward slashes, and the folders all of
// whose files it read. The build ID leaves it out. tifatest.StartPages compares it with the files
// as they are to tell a front end older than its sources.
const InputsFile = "inputs.json"

// inputs is the content of InputsFile.
type inputs struct {
	// Dirs are the public directory and content/, relative to the root: a file added to one of
	// them since is an input too.
	Dirs  []string          `json:"dirs"`
	Files map[string]string `json:"files"` // the hash of each file, see fileHash
	// Incomplete, when set, says why Files lacks files the build read, such as those Vite plugins
	// add with this.addWatchFile: ChangedInputs then fails.
	Incomplete string `json:"incomplete,omitempty"`
}

// projectFiles are the files at the root that shape a build besides the modules of the bundle:
// the packages, the Vite and TypeScript configs and tifa.yaml. Those that exist are recorded, and
// one that appears later counts as changed.
var projectFiles = []string{
	"package.json", "pnpm-lock.yaml", "package-lock.json", "yarn.lock", "bun.lock", "bun.lockb",
	"vite.config.ts", "vite.config.mts", "vite.config.js", "vite.config.mjs",
	"tsconfig.json", "tifa.yaml",
}

// writeInputs writes dist/inputs.json for the app at root: modules, the files the builds of the
// bundles read as the driver lists them, by absolute paths (those outside root are left out), the
// files of projectFiles, and every file of the public directory and of content/. incomplete says
// why modules lacks files the builds read, empty when it does not, see incompleteInputs.
func writeInputs(root, dist, public string, modules []string, incomplete string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if public, err = filepath.Abs(public); err != nil {
		return err
	}
	// Vite names modules by their real paths, such as /private/tmp/app for /tmp/app on macOS.
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	publicRel, err := filepath.Rel(root, public)
	if err != nil {
		return err
	}
	in := inputs{Dirs: []string{filepath.ToSlash(publicRel), "content"}, Files: map[string]string{}, Incomplete: incomplete}
	paths, err := scanInputs(root, in.Dirs)
	if err != nil {
		return err
	}
	for _, m := range modules {
		file := filepath.FromSlash(m)
		if !filepath.IsAbs(file) {
			return fmt.Errorf("the build read %s, which is not an absolute path", m)
		}
		rel, ok := under(real, file)
		if !ok {
			if rel, ok = under(root, file); !ok {
				continue // outside the root
			}
		}
		// Directories, which a plugin may watch, and files gone since have no hash.
		if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
			paths = append(paths, filepath.ToSlash(rel))
		}
	}
	for _, p := range paths {
		if in.Files[p], err = fileHash(filepath.Join(root, filepath.FromSlash(p))); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dist, InputsFile), append(data, '\n'), 0o644)
}

// incompleteInputs joins the reasons the driver gave for each build why the files it read are not
// all there, and when there is one, writes a warning to output. It returns the joined reasons, or
// "" when every build listed all its files.
func incompleteInputs(output io.Writer, reasons ...string) string {
	var why []string
	for _, r := range reasons {
		if r != "" && !slices.Contains(why, r) {
			why = append(why, r)
		}
	}
	if len(why) == 0 {
		return ""
	}
	joined := strings.Join(why, ", and ")
	fmt.Fprintf(output, "warning: %s does not list every file the build read (%s), so tifatest.StartPages fails with this build: upgrade tifa\n", InputsFile, joined)
	return joined
}

// under returns the path of file relative to dir, and whether file is in dir. Both are absolute.
func under(dir, file string) (string, bool) {
	rel, err := filepath.Rel(dir, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// scanInputs returns the inputs of the app at root that are not modules: the files of
// projectFiles that exist, the files of the _locales folders of app/ and every file under dirs, which are relative to root.
func scanInputs(root string, dirs []string) ([]string, error) {
	var paths []string
	for _, name := range projectFiles {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
			paths = append(paths, name)
		}
	}
	// The files of every _locales folder of app/, a folder that appeared since the build too.
	locales := filepath.Join(root, "app")
	err := filepath.WalkDir(locales, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == locales {
				return fs.SkipDir
			}
			return err
		}
		// The folders that hold no area for tifa build hold none here either.
		if d.IsDir() && path != locales && d.Name() != "_locales" && langs.SkippedName(d.Name()) {
			return fs.SkipDir
		}
		if d.Type().IsRegular() && filepath.Base(filepath.Dir(path)) == "_locales" {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, rel := range dirs {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && path == dir {
					return fs.SkipDir
				}
				return err
			}
			if d.Type().IsRegular() {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				paths = append(paths, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// ChangedInputs reads the inputs.json of dist and returns the files of the app at root that
// changed since the build, are gone, or were added where the build would have read them, sorted.
// It fails when dist has no inputs.json.
func ChangedInputs(root, dist string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dist, InputsFile))
	if err != nil {
		return nil, err
	}
	var in inputs
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("%s: %w", InputsFile, err)
	}
	if in.Files == nil {
		return nil, fmt.Errorf("%s has no files: run tifa build again", InputsFile)
	}
	if in.Incomplete != "" {
		return nil, fmt.Errorf("%s does not list every file the build read (%s), so it cannot tell a front end older than its sources: upgrade tifa, then run tifa build again", filepath.Join(dist, InputsFile), in.Incomplete)
	}
	var changed []string
	for p, want := range in.Files {
		got, err := fileHash(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if got != want {
			changed = append(changed, p)
		}
	}
	now, err := scanInputs(root, in.Dirs)
	if err != nil {
		return nil, err
	}
	for _, p := range now {
		if _, ok := in.Files[p]; !ok {
			changed = append(changed, p)
		}
	}
	slices.Sort(changed)
	return slices.Compact(changed), nil
}

// fileHash is the first 16 bytes of the SHA-256 of the file at path, in hex.
func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16]), nil
}
