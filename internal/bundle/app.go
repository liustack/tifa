// Package bundle builds the front end of an application with the application's own Vite, which
// runs on Node: the browser bundle, and the SSR bundle that QuickJS evaluates. Under tifa dev it
// also runs Vite's dev server. The Go side reads Vite's manifest and writes the tags a page loads
// its scripts and styles with.
package bundle

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Build is the content of dist/build.json, which the server reads at startup.
type Build struct {
	// ID identifies the build: a hash of every other file in dist, names included.
	ID string `json:"buildId"`
	// Head is the HTML that loads the browser bundle, written into the head of every page:
	// the stylesheet, the module preloads and the entry script. Empty without pages.
	Head string `json:"head"`
}

// clientEntry is the browser entry tifa generates, as Vite's manifest names it.
const clientEntry = ".tifa/client.ts"

// App writes the front end of the application at root into root/.tifa/dist, replacing what is
// there:
//
//	dist/assets/  the browser bundle, split per route, every name carrying a content hash, and
//	              a .gz of each text file worth compressing (see precompress)
//	dist/ssr.js   the SSR bundle, which knows the tags of the browser bundle as __TIFA_ASSETS__
//	dist/public/  a copy of the public directory, when it exists
//	dist/build.json
//
// Vite builds the two bundles with the application's vite.config.ts, which uses the tifa plugin
// (js/vite/plugin.js), on Node through n. pages is false for an application without route.tsx,
// which gets no bundles and needs no Node. The generated entries must exist under .tifa. The same
// input gives the same files and ID.
func App(n *Node, root, public string, pages bool) (string, error) {
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
		if err := n.call("client", nil, nil); err != nil {
			return "", err
		}
		if b.Head, err = readManifest(dist); err != nil {
			return "", err
		}
		if err := n.call("ssr", map[string]any{"assets": b.Head, "minify": true}, nil); err != nil {
			return "", err
		}
		if err := precompress(filepath.Join(dist, "assets")); err != nil {
			return "", err
		}
	}
	if err := copyDir(public, filepath.Join(dist, "public")); err != nil {
		return "", err
	}
	if b.ID, err = distID(dist, ""); err != nil {
		return "", err
	}
	return b.ID, writeBuild(dist, b)
}

// manifestEntry is one chunk or asset of Vite's manifest.
type manifestEntry struct {
	File    string   `json:"file"`
	Src     string   `json:"src"`
	IsEntry bool     `json:"isEntry"`
	Imports []string `json:"imports"`
	CSS     []string `json:"css"`
}

// readManifest reads the manifest Vite wrote into dist/.vite, removes it, so that it is not
// served or embedded, and returns the head tags of the browser entry: its stylesheets, a module
// preload for each chunk it imports, and the entry script.
func readManifest(dist string) (string, error) {
	dir := filepath.Join(dist, ".vite")
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", fmt.Errorf("reading Vite's manifest: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	var manifest map[string]manifestEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("Vite's manifest: %w", err)
	}
	return headTags(manifest)
}

// headTags writes the tags that load the entry of manifest, with every URL under /.
func headTags(manifest map[string]manifestEntry) (string, error) {
	entry, ok := manifest[clientEntry]
	if !ok || !entry.IsEntry {
		return "", fmt.Errorf("Vite's manifest has no entry %s, check the tifa plugin in vite.config.ts", clientEntry)
	}
	var styles, preloads []string
	seen := map[string]bool{}
	var visit func(key string, e manifestEntry)
	visit = func(key string, e manifestEntry) {
		if seen[key] {
			return
		}
		seen[key] = true
		for _, css := range e.CSS {
			if !slices.Contains(styles, css) {
				styles = append(styles, css)
			}
		}
		for _, imp := range e.Imports {
			if dep, ok := manifest[imp]; ok && !seen[imp] {
				preloads = append(preloads, dep.File)
				visit(imp, dep)
			}
		}
	}
	visit(clientEntry, entry)
	// With build.cssCodeSplit off, as the tifa plugin sets it, all CSS is one file of its own.
	if style, ok := manifest["style.css"]; ok && !slices.Contains(styles, style.File) {
		styles = append(styles, style.File)
	}
	var b strings.Builder
	for _, css := range styles {
		fmt.Fprintf(&b, `<link rel="stylesheet" href="%s">`, html.EscapeString("/"+css))
	}
	for _, file := range preloads {
		fmt.Fprintf(&b, `<link rel="modulepreload" href="%s">`, html.EscapeString("/"+file))
	}
	fmt.Fprintf(&b, `<script type="module" src="%s"></script>`, html.EscapeString("/"+entry.File))
	return b.String(), nil
}

func writeBuild(dist string, b Build) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dist, "build.json"), data, 0o644)
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

// distID hashes the relative name and content of every file under dist in sorted order, except
// skip, each prefixed with its length, and returns the first 16 hex digits.
func distID(dist, skip string) (string, error) {
	var names []string
	err := filepath.WalkDir(dist, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, err := filepath.Rel(dist, path)
			if name := filepath.ToSlash(rel); name != skip {
				names = append(names, name)
			}
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
