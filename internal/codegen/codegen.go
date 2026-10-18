// Package codegen writes the generated part of a tifa application into its .tifa directory: the
// Go main with the route registration, and the TypeScript route tree, the +route modules, the
// two entries, the tsconfig and the JavaScript runtime, and the profile go build optimizes with.
// It reads only the route manifest, the source files' exports, the project config and the
// profile. The output depends on nothing else, so the same input gives the same bytes.
package codegen

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/liustack/tifa/internal/analyze"
	tifaconfig "github.com/liustack/tifa/internal/config"
	"github.com/spf13/pflag"
)

// Dir is the directory, relative to the application root, that holds the generated files.
const Dir = ".tifa"

// Exports returns the names each of files exports, by file, such as bundle.Node.Exports. Types and
// names re-exported with export * from another module are left out.
type Exports func(files []string) (map[string][]string, error)

// Generate returns the generated files of the application at root, by path relative to Dir with
// forward slashes. dev selects the browser entry of tifa dev, which also loads the reload client
// of the dev server, and the main of tifa dev, which calls server.DevMain, and leaves out the
// profile, so tifa dev builds without PGO. exports reads the exports of route.tsx and layout.tsx.
// It is not called for an application without pages.
func Generate(root string, m *analyze.Manifest, dev bool, exports Exports) (map[string][]byte, error) {
	files := map[string][]byte{}
	configName, config, err := projectConfig(root)
	if err != nil {
		return nil, err
	}
	if configName != "" {
		files[configName] = config
	}
	// Check the file now rather than when the built program starts.
	settings, err := loadConfig(configName, config)
	if err != nil {
		return nil, err
	}
	target, err := viteTarget(settings.Build.Target)
	if err != nil {
		return nil, err
	}
	goFiles, err := generateGo(m, configName, dev)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, goFiles)
	tsFiles, err := generateTS(root, m, dev, exports, target)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tsFiles)
	if !dev {
		_, pages := files["client.ts"]
		pgo, err := profile(root, settings.Build.PGO, pages)
		if err != nil {
			return nil, err
		}
		if pgo != nil {
			files[pgoFile] = pgo
		}
	}
	return files, nil
}

// BuildSettings returns the build section of the project tifa.yaml or tifa.json, the defaults
// without one.
func BuildSettings(root string) (tifaconfig.Build, error) {
	name, data, err := projectConfig(root)
	if err != nil {
		return tifaconfig.Build{}, err
	}
	cfg, err := loadConfig(name, data)
	return cfg.Build, err
}

// ProjectSettings returns the whole project configuration from tifa.yaml or tifa.json, the
// defaults without one. tifa dev reads the server section to know how long the app may take to stop.
func ProjectSettings(root string) (tifaconfig.Config, error) {
	name, data, err := projectConfig(root)
	if err != nil {
		return tifaconfig.Config{}, err
	}
	return loadConfig(name, data)
}

// loadConfig reads a project config file the way the built program does, with no other layer.
func loadConfig(name string, data []byte) (tifaconfig.Config, error) {
	fs := pflag.NewFlagSet("tifa", pflag.ContinueOnError)
	tifaconfig.AddFlags(fs)
	if err := fs.Parse(nil); err != nil {
		return tifaconfig.Config{}, err
	}
	return tifaconfig.Load(fs, nil, name, data)
}

// projectConfig reads tifa.yaml or tifa.json from the application root. The generated main embeds
// a copy, because go:embed cannot reach a parent directory.
func projectConfig(root string) (name string, data []byte, err error) {
	for _, candidate := range []string{"tifa.yaml", "tifa.json"} {
		content, err := os.ReadFile(filepath.Join(root, candidate))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		if name != "" {
			return "", nil, fmt.Errorf("%s: both tifa.yaml and tifa.json exist, keep one", root)
		}
		name, data = candidate, content
	}
	return name, data, nil
}

// DistDir is the directory under Dir that the bundle step owns. Write leaves it alone.
const DistDir = "dist"

// Write makes root/.tifa hold exactly files besides dist/: it writes the files whose content
// changed and removes the files that are no longer generated, so tools watching the directory
// see only real changes.
func Write(root string, files map[string][]byte) error {
	dir := filepath.Join(root, Dir)
	var stale []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == dir {
			return filepath.SkipAll
		}
		if err != nil {
			return err
		}
		if d.IsDir() && path == filepath.Join(dir, DistDir) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if _, ok := files[filepath.ToSlash(rel)]; !ok {
			stale = append(stale, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if err := removeEmptyDirs(dir); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, files[name]) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// removeEmptyDirs removes the directories under dir that hold no files, deepest first.
func removeEmptyDirs(dir string) error {
	var dirs []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == dir {
			return filepath.SkipAll
		}
		if err == nil && d.IsDir() && path != dir {
			dirs = append(dirs, path)
		}
		return err
	})
	if err != nil {
		return err
	}
	for _, d := range slices.Backward(dirs) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(d); err != nil {
				return err
			}
		}
	}
	return nil
}
