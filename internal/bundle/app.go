package bundle

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"

	"github.com/evanw/esbuild/pkg/api"
)

// Build is the content of dist/build.json, which the server reads at startup.
type Build struct {
	// ID identifies the build: a hash of every other file in dist, names included.
	ID string `json:"buildId"`
	// Script and Style are the URL paths of the browser entry and its CSS, empty without pages.
	Script string `json:"script"`
	Style  string `json:"style"`
}

// App writes the front end of the application at root into root/.tifa/dist, replacing what is
// there:
//
//	dist/assets/  the browser bundle, split per route, every name carrying a content hash
//	dist/ssr.js   the SSR bundle, which knows the browser assets through __TIFA_ASSETS__
//	dist/public/  a copy of the public directory, when it exists
//	dist/build.json
//
// Stylesheets that use Tailwind are compiled by the application's Tailwind CLI (tailwindPlugin).
// target is build.target and applies to the browser bundle only. The SSR bundle runs in
// QuickJS, never in a browser, and keeps ES2022, which QuickJS runs as it is: a lower target
// would only make rendering slower, a higher one could emit syntax QuickJS lacks.
// pages is false for an application without route.tsx, which gets no bundles. The generated
// entries and tsconfig must exist under .tifa. The same input gives the same files and ID.
func App(root, public, target string, pages bool) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	dist := filepath.Join(root, ".tifa", "dist")
	if err := os.RemoveAll(dist); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return "", err
	}
	var b Build
	if pages {
		generated, err := Generated(root)
		if err != nil {
			return "", err
		}
		tsconfig := filepath.Join(".tifa", "tsconfig.json")
		var tailwindTime atomic.Int64
		assets, _, err := Client(ClientOptions{
			Root: root, Entry: ".tifa/client.ts", Outdir: filepath.Join(dist, "assets"),
			Tsconfig: tsconfig, Write: true, Target: target,
			Plugins: []api.Plugin{generated, tailwindPlugin(root, &tailwindTime)},
		})
		if err != nil {
			return "", err
		}
		b.Script = "/assets/" + assets.Script
		if assets.Style != "" {
			b.Style = "/assets/" + assets.Style
		}
		define, err := json.Marshal(map[string]string{"script": b.Script, "style": b.Style})
		if err != nil {
			return "", err
		}
		if _, err := SSR(SSROptions{
			Root: root, Entry: ".tifa/server.ts", Outfile: filepath.Join(dist, "ssr.js"),
			Tsconfig: tsconfig, Write: true, Plugins: []api.Plugin{generated},
			Define: map[string]string{"__TIFA_ASSETS__": string(define)},
		}); err != nil {
			return "", err
		}
	}
	if err := copyDir(public, filepath.Join(dist, "public")); err != nil {
		return "", err
	}
	if b.ID, err = distID(dist); err != nil {
		return "", err
	}
	data, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return b.ID, os.WriteFile(filepath.Join(dist, "build.json"), data, 0o644)
}

// copyDir copies the regular files under src to dst. A missing src copies nothing.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == src {
			return filepath.SkipAll
		}
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// distID hashes the relative name and content of every file under dist in sorted order, each
// prefixed with its length, and returns the first 16 hex digits.
func distID(dist string) (string, error) {
	var names []string
	err := filepath.WalkDir(dist, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, err := filepath.Rel(dist, path)
			names = append(names, filepath.ToSlash(rel))
			return err
		}
		return err
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dist, filepath.FromSlash(name)))
		if err != nil {
			return "", err
		}
		// Each name and content is preceded by its length, so the byte stream spells one
		// file tree only, whatever the files hold.
		hash.Write(binary.BigEndian.AppendUint64(nil, uint64(len(name))))
		hash.Write([]byte(name))
		hash.Write(binary.BigEndian.AppendUint64(nil, uint64(len(data))))
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16], nil
}
