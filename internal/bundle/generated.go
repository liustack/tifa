package bundle

import (
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// Generated resolves the modules that tifa build writes into root/.tifa, the way the generated
// tsconfig lets TypeScript see them: a relative import of +route from app/<dir> (./+route, or
// ../+route for an ancestor Layout) is .tifa/app/<dir>/+route.ts, and tifa/client is the runtime.
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
		b.OnResolve(api.OnResolveOptions{Filter: `^\.\.?/(\.\./)*\+route$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			target := filepath.Join(args.ResolveDir, filepath.FromSlash(args.Path))
			rel, err := filepath.Rel(root, target)
			if err != nil || !strings.HasPrefix(target, app+string(filepath.Separator)) {
				return api.OnResolveResult{Errors: []api.Message{{
					Text: args.Path + " is generated for directories under app/ only, " + args.Importer + " is outside",
				}}}, nil
			}
			return api.OnResolveResult{Path: filepath.Join(gen, rel+".ts")}, nil
		})
		b.OnResolve(api.OnResolveOptions{Filter: `^tifa/client$`}, func(api.OnResolveArgs) (api.OnResolveResult, error) {
			return api.OnResolveResult{Path: filepath.Join(gen, "runtime", "client.ts")}, nil
		})
	}}, nil
}
