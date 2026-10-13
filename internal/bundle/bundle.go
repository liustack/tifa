// Package bundle builds the two JavaScript bundles of an application with the
// esbuild Go library: the SSR bundle that QuickJS evaluates and the browser
// bundle. It needs no Node at build time. Node only installs node_modules.
package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// metafile is the part of esbuild's metafile that the build checks read.
type metafile struct {
	Inputs map[string]struct {
		Imports []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"imports"`
	} `json:"inputs"`
	Outputs map[string]struct {
		Inputs  map[string]any `json:"inputs"`
		Imports []struct {
			Path string `json:"path"`
		} `json:"imports"`
		EntryPoint string `json:"entryPoint"`
		CSSBundle  string `json:"cssBundle"`
	} `json:"outputs"`
}

// run executes one esbuild build and turns its error messages into a Go error.
// Warnings stay in the result for the caller to print.
func run(o api.BuildOptions) (api.BuildResult, metafile, error) {
	result := api.Build(o)
	var meta metafile
	if len(result.Errors) > 0 {
		text := api.FormatMessages(result.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage})
		return result, meta, errors.New("esbuild: " + strings.TrimSpace(strings.Join(text, "")))
	}
	if err := json.Unmarshal([]byte(result.Metafile), &meta); err != nil {
		return result, meta, err
	}
	return result, meta, nil
}

// buildOptions fills what both bundles share: project root, entry,
// tsconfig, ES2022 target, minification and the production NODE_ENV.
func buildOptions(root, entry, tsconfig string, write bool, plugins []api.Plugin) (api.BuildOptions, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return api.BuildOptions{}, err
	}
	if tsconfig != "" && !filepath.IsAbs(tsconfig) {
		tsconfig = filepath.Join(root, tsconfig)
	}
	return api.BuildOptions{
		AbsWorkingDir:     root,
		EntryPoints:       []string{entry},
		Tsconfig:          tsconfig,
		Bundle:            true,
		Target:            api.ES2022,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Metafile:          true,
		Write:             write,
		LogLevel:          api.LogLevelSilent,
		Define:            map[string]string{"process.env.NODE_ENV": `"production"`},
		Plugins:           plugins,
	}, nil
}

// BuildID hashes every file under dir in sorted path order and returns the
// first 16 hex digits of the SHA-256. The browser compares it with the server
// to detect a new deployment.
func BuildID(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	hash := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16], nil
}
