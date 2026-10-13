package bundle

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// ClientOptions configures the browser bundle. Relative paths are relative to Root.
type ClientOptions struct {
	Root     string // project root, the directory holding node_modules
	Entry    string // browser entry, such as the generated client.tsx
	Outdir   string // output directory for the entry, lazy chunks and CSS
	Tsconfig string // empty lets esbuild find the nearest tsconfig.json
	Write    bool   // false keeps the output in memory only
	// Plugins run before esbuild's own resolution, for example to resolve
	// generated framework modules.
	Plugins []api.Plugin
}

// Assets names the files a page must reference, relative to Outdir.
type Assets struct {
	Script string // the ES module entry
	Style  string // the CSS bundle, empty when the entry imports no CSS
}

// Client builds the browser bundle: ES modules split at dynamic imports, with
// content hashes in every file name so the files can be cached forever.
func Client(o ClientOptions) (Assets, api.BuildResult, error) {
	opts, err := clientBuildOptions(o)
	if err != nil {
		return Assets{}, api.BuildResult{}, err
	}
	result, meta, err := run(opts)
	if err != nil {
		return Assets{}, result, err
	}
	assets, err := clientAssets(opts, meta, o.Entry)
	return assets, result, err
}

func clientBuildOptions(o ClientOptions) (api.BuildOptions, error) {
	opts, err := buildOptions(o.Root, o.Entry, o.Tsconfig, o.Write, o.Plugins)
	if err != nil {
		return opts, err
	}
	opts.Outdir = o.Outdir
	opts.Platform = api.PlatformBrowser
	opts.Format = api.FormatESModule
	opts.Splitting = true
	opts.EntryNames = "app-[hash]"
	opts.ChunkNames = "chunk-[hash]"
	return opts, nil
}

// clientAssets finds the entry script and its CSS among the outputs of a browser build.
func clientAssets(opts api.BuildOptions, meta metafile, entry string) (Assets, error) {
	outdir := opts.Outdir
	if !filepath.IsAbs(outdir) {
		outdir = filepath.Join(opts.AbsWorkingDir, outdir)
	}
	rel := func(output string) (string, error) {
		path, err := filepath.Rel(outdir, filepath.Join(opts.AbsWorkingDir, filepath.FromSlash(output)))
		return filepath.ToSlash(path), err
	}
	// With splitting, every dynamically imported module is an entry point in
	// the metafile too. The entry is the one entry point nothing imports
	// dynamically. Comparing with the entry option would miss an entry that esbuild or
	// a plugin resolved to another path, such as one without its extension.
	dynamic := map[string]bool{}
	for _, input := range meta.Inputs {
		for _, imp := range input.Imports {
			if imp.Kind == "dynamic-import" {
				dynamic[imp.Path] = true
			}
		}
	}
	var entries []string
	for name, output := range meta.Outputs {
		if output.EntryPoint != "" && !dynamic[output.EntryPoint] && !strings.HasSuffix(name, ".css") {
			entries = append(entries, name)
		}
	}
	if len(entries) != 1 {
		return Assets{}, fmt.Errorf("browser bundle has %d outputs for entry %s, want 1: %v", len(entries), entry, entries)
	}
	var assets Assets
	var err error
	if assets.Script, err = rel(entries[0]); err != nil {
		return Assets{}, err
	}
	if css := meta.Outputs[entries[0]].CSSBundle; css != "" {
		if assets.Style, err = rel(css); err != nil {
			return Assets{}, err
		}
	}
	return assets, nil
}
