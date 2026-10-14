package bundle

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// Generated resolves the modules that tifa build writes into root/.tifa, the way the generated
// tsconfig lets TypeScript see them: rootDirs merges root and root/.tifa, so a relative import
// whose last element is +route or +layout, with or without .js or .ts, and which points into
// app/<dir> is .tifa/app/<dir>/+route.ts or +layout.ts, whatever the path in between.
// tifa/client is the runtime.
// root is made absolute and resolved through symbolic links first, because esbuild reports real paths.
func Generated(root string) (api.Plugin, error) {
	root, err := filepath.Abs(root)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		return api.Plugin{}, err
	}
	gen := filepath.Join(root, ".tifa")
	app := filepath.Join(root, "app")
	return api.Plugin{Name: "tifa-generated", Setup: func(b api.PluginBuild) {
		b.OnResolve(api.OnResolveOptions{Filter: `^\.\.?/(.*/)?\+(route|layout)(\.js|\.ts)?$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			target := filepath.Join(args.ResolveDir, filepath.FromSlash(args.Path))
			target = strings.TrimSuffix(strings.TrimSuffix(target, ".js"), ".ts")
			rel, err := filepath.Rel(root, target)
			if err != nil || !strings.HasPrefix(target, app+string(filepath.Separator)) {
				return api.OnResolveResult{Errors: []api.Message{{
					Text: args.Path + " is generated for directories under app/ only, " + args.Importer + " is outside",
				}}}, nil
			}
			module := filepath.Join(gen, rel+".ts")
			if _, err := os.Stat(module); err != nil {
				return api.OnResolveResult{Errors: []api.Message{{
					Text: args.Path + ": " + filepath.ToSlash(filepath.Dir(rel)) + " has no generated " + filepath.Base(rel) + " module: +route exists for a page, +layout for a Layout with pages",
				}}}, nil
			}
			return api.OnResolveResult{Path: module}, nil
		})
		b.OnResolve(api.OnResolveOptions{Filter: `^tifa/client$`}, func(api.OnResolveArgs) (api.OnResolveResult, error) {
			return api.OnResolveResult{Path: filepath.Join(gen, "runtime", "client.ts")}, nil
		})
	}}, nil
}
