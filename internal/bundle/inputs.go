package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// InputsFile is the file of dist that records what a build was made from: the hash of each file it
// read, by its path relative to the application root with forward slashes. The build ID leaves it
// out. tifatest.StartPages compares it with the files as they are to tell a front end older than
// its sources.
const InputsFile = "inputs.json"

// projectFiles are the files at the root that shape a build besides the modules of the bundle:
// the packages, the Vite and TypeScript configs and tifa.yaml. Those that exist are recorded.
var projectFiles = []string{
	"package.json", "pnpm-lock.yaml", "package-lock.json", "yarn.lock", "bun.lock", "bun.lockb",
	"vite.config.ts", "vite.config.mts", "vite.config.js", "vite.config.mjs",
	"tsconfig.json", "tifa.yaml",
}

// writeInputs writes dist/inputs.json for the app at root: modules, the files of the modules of
// the SSR bundle as the driver lists them (those outside root are left out), the files of
// projectFiles, and every file of the public directory and of content/.
func writeInputs(root, dist, public string, modules []string) error {
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
	var paths []string
	for _, m := range modules {
		rel, err := filepath.Rel(real, filepath.FromSlash(m))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if info, err := os.Stat(m); err == nil && info.Mode().IsRegular() {
			paths = append(paths, filepath.ToSlash(rel))
		}
	}
	for _, name := range projectFiles {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
			paths = append(paths, name)
		}
	}
	for _, dir := range []string{public, filepath.Join(root, "content")} {
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
			return err
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	hashes := make(map[string]string, len(paths))
	for _, p := range paths {
		h, err := fileHash(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return err
		}
		hashes[p] = h
	}
	data, err := json.MarshalIndent(hashes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dist, InputsFile), append(data, '\n'), 0o644)
}

// ChangedInputs reads the inputs.json of dist and returns the files of the app at root that
// changed since the build, or are gone, sorted. It fails when dist has no inputs.json.
func ChangedInputs(root, dist string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dist, InputsFile))
	if err != nil {
		return nil, err
	}
	var hashes map[string]string
	if err := json.Unmarshal(data, &hashes); err != nil {
		return nil, err
	}
	var changed []string
	for p, want := range hashes {
		got, err := fileHash(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if got != want {
			changed = append(changed, p)
		}
	}
	slices.Sort(changed)
	return changed, nil
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
