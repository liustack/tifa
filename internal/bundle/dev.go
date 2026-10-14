package bundle

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/evanw/esbuild/pkg/api"
)

// devAssets is the global the SSR bundle of tifa dev reads the browser assets from. App puts the
// assets into the bundle as a constant, which would change the SSR build options whenever the
// browser entry gets a new hash. Dev keeps the options fixed and writes the value in front of
// the bundle instead, so both esbuild contexts stay incremental.
const devAssets = "globalThis.__tifaAssets"

// Dev builds the front end of an application like App, into the same .tifa/dist, for tifa dev.
// It keeps one esbuild context per bundle, so a rebuild parses only what changed.
// A Dev is used by one goroutine at a time.
type Dev struct {
	root       string
	dist       string
	generated  api.Plugin
	client     api.BuildContext // nil while the application has no pages
	clientOpts api.BuildOptions
	ssr        api.BuildContext
	target     string
}

// NewDev prepares the incremental builds of the application at root. target is build.target,
// as App takes it. It is read once, a change applies when tifa dev starts again.
func NewDev(root, target string) (*Dev, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	generated, err := Generated(root)
	if err != nil {
		return nil, err
	}
	return &Dev{root: root, dist: filepath.Join(root, ".tifa", "dist"), generated: generated, target: target}, nil
}

// Close releases the esbuild contexts.
func (d *Dev) Close() {
	if d.client != nil {
		d.client.Dispose()
		d.ssr.Dispose()
		d.client, d.ssr = nil, nil
	}
}

// Build brings .tifa/dist up to date and returns the build ID, like App. pages is false for an
// application without route.tsx, which gets no bundles.
func (d *Dev) Build(public string, pages bool) (string, error) {
	if err := os.MkdirAll(d.dist, 0o755); err != nil {
		return "", err
	}
	var b Build
	if pages {
		var err error
		if b, err = d.bundle(); err != nil {
			return "", err
		}
	} else {
		d.Close()
		for _, name := range []string{"assets", "ssr.js"} {
			if err := os.RemoveAll(filepath.Join(d.dist, name)); err != nil {
				return "", err
			}
		}
	}
	if err := os.RemoveAll(filepath.Join(d.dist, "public")); err != nil {
		return "", err
	}
	if err := copyDir(public, filepath.Join(d.dist, "public")); err != nil {
		return "", err
	}
	buildJSON := filepath.Join(d.dist, "build.json")
	if err := os.Remove(buildJSON); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	var err error
	if b.ID, err = distID(d.dist); err != nil {
		return "", err
	}
	data, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return b.ID, os.WriteFile(buildJSON, data, 0o644)
}

// bundle rebuilds both bundles and writes them into dist.
func (d *Dev) bundle() (Build, error) {
	if d.client == nil {
		if err := d.start(); err != nil {
			return Build{}, err
		}
	}
	result := d.client.Rebuild()
	meta, err := readResult(result)
	if err != nil {
		return Build{}, err
	}
	assets, err := clientAssets(d.clientOpts, meta, d.clientOpts.EntryPoints[0])
	if err != nil {
		return Build{}, err
	}
	if err := replaceDir(filepath.Join(d.dist, "assets"), result.OutputFiles); err != nil {
		return Build{}, err
	}
	b := Build{Script: "/assets/" + assets.Script}
	if assets.Style != "" {
		b.Style = "/assets/" + assets.Style
	}

	result = d.ssr.Rebuild()
	if meta, err = readResult(result); err != nil {
		return Build{}, err
	}
	if err := checkSSR(meta); err != nil {
		return Build{}, err
	}
	if len(result.OutputFiles) != 1 {
		return Build{}, errors.New("the SSR build wrote no single file")
	}
	define, err := json.Marshal(map[string]string{"script": b.Script, "style": b.Style})
	if err != nil {
		return Build{}, err
	}
	ssr := append([]byte(devAssets+"="+string(define)+";\n"), result.OutputFiles[0].Contents...)
	return b, os.WriteFile(filepath.Join(d.dist, "ssr.js"), ssr, 0o644)
}

// start creates the esbuild contexts with the options App uses, except that nothing is written
// by esbuild itself and the SSR bundle reads the assets from devAssets.
func (d *Dev) start() error {
	tsconfig := filepath.Join(".tifa", "tsconfig.json")
	var err error
	d.clientOpts, err = clientBuildOptions(ClientOptions{
		Root: d.root, Entry: ".tifa/client.ts", Outdir: filepath.Join(d.dist, "assets"),
		Tsconfig: tsconfig, Target: d.target, Plugins: []api.Plugin{d.generated},
	})
	if err != nil {
		return err
	}
	ssrOpts, err := ssrBuildOptions(SSROptions{
		Root: d.root, Entry: ".tifa/server.ts", Outfile: filepath.Join(d.dist, "ssr.js"),
		Tsconfig: tsconfig, Plugins: []api.Plugin{d.generated},
		Define: map[string]string{"__TIFA_ASSETS__": devAssets},
	}, true)
	if err != nil {
		return err
	}
	client, ctxErr := api.Context(d.clientOpts)
	if ctxErr != nil {
		return ctxErr
	}
	ssr, ctxErr := api.Context(ssrOpts)
	if ctxErr != nil {
		client.Dispose()
		return ctxErr
	}
	d.client, d.ssr = client, ssr
	return nil
}

// replaceDir makes dir hold exactly files, writing only those whose content changed.
func replaceDir(dir string, files []api.OutputFile) error {
	keep := map[string]bool{}
	for _, f := range files {
		keep[f.Path] = true
		if old, err := os.ReadFile(f.Path); err == nil && string(old) == string(f.Contents) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.Path, f.Contents, 0o644); err != nil {
			return err
		}
	}
	return filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || keep[path] {
			return err
		}
		return os.Remove(path)
	})
}
