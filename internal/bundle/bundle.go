// Package bundle builds the two JavaScript bundles of an application with the
// esbuild Go library: the SSR bundle that QuickJS evaluates and the browser
// bundle.
package bundle

import (
	"encoding/json"
	"errors"
	"path/filepath"
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
	meta, err := readResult(result)
	return result, meta, err
}

// readResult turns the error messages of a build into a Go error, or returns its metafile.
func readResult(result api.BuildResult) (metafile, error) {
	var meta metafile
	if len(result.Errors) > 0 {
		text := api.FormatMessages(result.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage})
		return meta, errors.New("esbuild: " + strings.TrimSpace(strings.Join(text, "")))
	}
	err := json.Unmarshal([]byte(result.Metafile), &meta)
	return meta, err
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
