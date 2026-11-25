// Package bundle builds the front end of an application with the application's own Vite, which
// runs on Node: the browser bundle, and the SSR bundle that QuickJS evaluates. Under tifa dev it
// also runs Vite's dev server. The Go side reads Vite's manifest and writes the tags a page loads
// its scripts and styles with.
package bundle

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liustack/tifa/internal/ssr"
	"github.com/liustack/tifa/internal/symwalk"
)

// Build is the content of dist/build.json, which the server reads at startup.
type Build struct {
	// ID identifies the build: a hash of every other file in dist, names included.
	ID string `json:"buildId"`
	// Head is the HTML that loads the browser bundle, written into the head of every page:
	// the stylesheet, the module preloads and the entry script. Empty without pages.
	Head string `json:"head"`
	// Preload lists, by the ID of a Layout or route whose module is a chunk of its own, the URLs
	// of the JavaScript files the module needs that Head does not load: its chunk, then the
	// chunks it imports statically. A page preloads those of its Layouts and route.
	Preload map[string][]string `json:"preload,omitempty"`
	// Public holds the content hash of each file of dist/public by its path in that folder. The
	// server makes it the ETag of the file: the copy embedded in the program has no
	// modification time, so a browser could not revalidate the file without it. tifa dev leaves
	// it out, and its files keep the modification time of the directory it serves.
	Public map[string]string `json:"public,omitempty"`
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
//	dist/content.json  the entries of the content collections, with the headings of each body
//	dist/content/  the body of each entry as HTML, which the program reads and never serves
//	dist/build.json  with the content hash of each public file, see Build.Public
//
// Vite builds the two bundles with the application's vite.config.ts, which uses the tifa plugin
// (js/vite/plugin.js), on Node through n. pages is false for an application without page.tsx,
// which gets no bundles and needs no Node. The generated entries must exist under .tifa. The same
// input gives the same files and ID.
//
// dotfiles is build.public_dotfiles, the dot names of public that are copied, see ScanPublic.
//
// c holds the content collections, nil without any: dist/content.json with their entries and the
// bodies in dist/content/, which the tifa plugin converts during the browser build. They count in
// the ID.
func App(n *Node, root, public string, dotfiles []string, pages bool, c *Content) (string, error) {
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
	if err := prepareContent(root, c, pages); err != nil {
		return "", err
	}
	var b Build
	// The modules of both bundles are inputs: a component that only the browser loads is a stub in
	// the SSR bundle, and the browser bundle has no module that only the server renders.
	var clientOut, ssrOut struct {
		Modules    []string `json:"modules"`
		Incomplete string   `json:"incomplete"`
	}
	if pages {
		if err := n.call("client", nil, &clientOut); err != nil {
			return "", err
		}
		if b.Head, b.Preload, err = readManifest(dist); err != nil {
			return "", err
		}
		if err := n.call("ssr", map[string]any{"assets": b.Head, "minify": true}, &ssrOut); err != nil {
			return "", err
		}
		if err := checkSSR(dist); err != nil {
			return "", err
		}
		if err := precompress(filepath.Join(dist, "assets")); err != nil {
			return "", err
		}
	} else if c != nil && len(c.Markdown) > 0 {
		if err := convertAlone(n, c, "tifa build"); err != nil {
			return "", err
		}
		if err := precompress(filepath.Join(dist, "assets")); err != nil {
			return "", err
		}
	}
	if err := syncDir(public, filepath.Join(dist, "public"), dotfiles); err != nil {
		return "", err
	}
	if b.Public, err = publicHashes(filepath.Join(dist, "public")); err != nil {
		return "", err
	}
	if err := finishContent(root, dist, c); err != nil {
		return "", err
	}
	if b.ID, err = distID(dist, func(name string) bool { return name == InputsFile }); err != nil {
		return "", err
	}
	incomplete := incompleteInputs(n.output, clientOut.Incomplete, ssrOut.Incomplete)
	if err := writeInputs(root, dist, public, append(clientOut.Modules, ssrOut.Modules...), incomplete); err != nil {
		return "", err
	}
	return b.ID, writeBuild(dist, b)
}

// checkSSR compiles dist/ssr.js as the server does, without running it. The bundle is an ES
// module, strict mode code, and rolldown keeps code of a CommonJS file that only sloppy mode
// allows, such as a with statement: that fails here, with its place in the sources, and not
// when the server starts.
func checkSSR(dist string) error {
	code, err := os.ReadFile(filepath.Join(dist, "ssr.js"))
	if err != nil {
		return err
	}
	sourceMap, err := os.ReadFile(filepath.Join(dist, "ssr.js.map"))
	if errors.Is(err, fs.ErrNotExist) {
		sourceMap = nil
	} else if err != nil {
		return err
	}
	return ssr.Check(string(code), sourceMap)
}

// manifestEntry is one chunk or asset of Vite's manifest.
type manifestEntry struct {
	File           string   `json:"file"`
	Src            string   `json:"src"`
	IsEntry        bool     `json:"isEntry"`
	Imports        []string `json:"imports"`
	DynamicImports []string `json:"dynamicImports"`
	CSS            []string `json:"css"`
}

// readManifest reads the manifest Vite wrote into dist/.vite, removes it, so that it is not
// served or embedded, and returns the head tags of the browser entry (its stylesheets, a module
// preload for each chunk it imports, and the entry script) and the preloads of the route
// modules (see routePreloads).
func readManifest(dist string) (string, map[string][]string, error) {
	dir := filepath.Join(dist, ".vite")
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", nil, fmt.Errorf("reading Vite's manifest: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", nil, err
	}
	var manifest map[string]manifestEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", nil, fmt.Errorf("the manifest of Vite: %w", err)
	}
	head, err := headTags(manifest)
	if err != nil {
		return "", nil, err
	}
	return head, routePreloads(manifest), nil
}

// staticImports calls f for key and then for each chunk it imports statically, depth first and
// each once, skipping the keys seen holds already. It marks what it visits in seen.
func staticImports(manifest map[string]manifestEntry, key string, seen map[string]bool, f func(key string, e manifestEntry)) {
	e, ok := manifest[key]
	if !ok || seen[key] {
		return
	}
	seen[key] = true
	f(key, e)
	for _, imp := range e.Imports {
		staticImports(manifest, imp, seen, f)
	}
}

// routePreloads lists, for each layout.tsx, layout_.tsx and page.tsx under app/ that is a chunk of its own,
// the URLs of the chunk and of the chunks it imports statically, in the order headTags writes
// them, without the entry and what it imports. The key is the ID of the Layout or route: the
// path without .tsx for a Layout, and the directory followed by /route for a page. A module the entry imports, such as the root Layout, gets nothing, and
// neither do the modules a chunk imports with import(), which load when their code runs.
func routePreloads(manifest map[string]manifestEntry) map[string][]string {
	inHead := map[string]bool{}
	staticImports(manifest, clientEntry, inHead, func(string, manifestEntry) {})
	preload := map[string][]string{}
	for key := range manifest {
		name := path.Base(key)
		if !strings.HasPrefix(key, "app/") || (name != "page.tsx" && name != "layout.tsx" && name != "layout_.tsx") || inHead[key] {
			continue
		}
		seen := maps.Clone(inHead)
		var files []string
		staticImports(manifest, key, seen, func(_ string, e manifestEntry) {
			if strings.HasSuffix(e.File, ".js") {
				files = append(files, "/"+e.File)
			}
		})
		if len(files) > 0 {
			preload[routeID(key)] = files
		}
	}
	return preload
}

// headTags writes the tags that load the entry of manifest, with every URL under /.
func headTags(manifest map[string]manifestEntry) (string, error) {
	entry, ok := manifest[clientEntry]
	if !ok || !entry.IsEntry {
		return "", fmt.Errorf("the manifest of Vite has no entry %s, check the tifa plugin in vite.config.ts", clientEntry)
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

// writeBuild writes build.json with its map keys sorted, so the same build makes the same file,
// and with it the same program.
func writeBuild(dist string, b Build) error {
	data, err := json.Marshal(b, json.Deterministic(true))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dist, "build.json"), data, 0o644)
}

// publicHashes returns the content hash of each regular file under dir by its slash-separated
// path in dir, the first 16 bytes of its SHA-256 in hex. It is nil for a folder without files.
func publicHashes(dir string) (map[string]string, error) {
	var hashes map[string]string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == dir {
			return filepath.SkipAll
		}
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hashes == nil {
			hashes = map[string]string{}
		}
		hashes[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:16])
		return nil
	})
	return hashes, err
}

// PublicScan is what a public folder holds for tifa build and tifa dev: the files they copy and
// the dot names they leave out.
type PublicScan struct {
	// Files and Dirs are the regular files and the folders to copy, by their paths from the public
	// folder, with the folder itself as ".".
	Files map[string]fs.FileInfo
	Dirs  map[string]bool
	// LeftOut lists the files and folders whose names start with a dot and that build.public_dotfiles
	// does not name, by their paths from the public folder with /. A folder ends in / and counts
	// once, its files are not listed. They are in the order of the walk.
	LeftOut []string
}

// ScanPublic walks the public folder src, following links. It skips a file or folder whose name
// starts with a dot, apart from .well-known and the paths in dotfiles (build.public_dotfiles):
// a .DS_Store or a .env left in public/ is not published. A skipped folder is not entered, and a
// folder that dotfiles names is, with its dot names inside it still skipped unless named too. A
// missing src is a folder without files. It is an error when dotfiles names a path that is not in
// src, or is in a dot folder that is not named. It is the one rule of which files of public/
// count, also for tifa routes.
func ScanPublic(src string, dotfiles []string) (*PublicScan, error) {
	scan := &PublicScan{Files: map[string]fs.FileInfo{}, Dirs: map[string]bool{}}
	named := map[string]bool{}
	for _, p := range dotfiles {
		named[p] = false
	}
	err := symwalk.Walk(src, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == src {
			return filepath.SkipAll
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if name := d.Name(); path != src && strings.HasPrefix(name, ".") && name != ".well-known" {
			if _, ok := named[rel]; ok {
				named[rel] = true
			} else if d.IsDir() {
				scan.LeftOut = append(scan.LeftOut, rel+"/")
				return filepath.SkipDir
			} else {
				scan.LeftOut = append(scan.LeftOut, rel)
				return nil
			}
		}
		if d.IsDir() {
			scan.Dirs[rel] = true
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		scan.Files[rel] = info
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, p := range dotfiles {
		if named[p] {
			continue
		}
		if _, err := os.Stat(filepath.Join(src, filepath.FromSlash(p))); err != nil {
			return nil, fmt.Errorf("build.public_dotfiles: %q is not in public/", p)
		}
		// It is there and was not reached: a dot folder above it is left out.
		parts := strings.Split(p, "/")
		for i := 1; i < len(parts); i++ {
			if above := strings.Join(parts[:i], "/"); strings.HasPrefix(parts[i-1], ".") && parts[i-1] != ".well-known" && !slices.Contains(dotfiles, above) {
				return nil, fmt.Errorf("build.public_dotfiles: %q is inside %q, which is left out unless build.public_dotfiles names it too", p, above)
			}
		}
		return nil, fmt.Errorf("build.public_dotfiles: %q is not a file or folder tifa build copies", p)
	}
	return scan, nil
}

// syncDir makes the regular files under dst the files under src, following links: it copies the
// ones that are new or whose size or modification time differ, and removes the ones src lacks,
// with the folders that empties. A file that is up to date is not touched, so a folder of large
// files costs a walk and not a copy. A copy keeps the modification time of its source, which is
// what the next call compares. A missing src is a folder without files. The files of dst are
// replaced by renaming a finished copy over them, so a reader sees the old file or the new one.
// Which files count is ScanPublic: dot names are left out unless dotfiles names them.
func syncDir(src, dst string, dotfiles []string) error {
	scan, err := ScanPublic(src, dotfiles)
	if err != nil {
		return err
	}
	files, dirs := scan.Files, scan.Dirs
	// Remove what src lacks, or has as the other kind: a file where it has a folder and the reverse.
	err = filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == dst {
			return filepath.SkipAll
		}
		if err != nil || path == dst {
			return err
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil {
			return err
		}
		if d.IsDir() && dirs[rel] || d.Type().IsRegular() && files[rel] != nil {
			return nil
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		target := filepath.Join(dst, rel)
		if cur, err := os.Lstat(target); err == nil && cur.Mode().IsRegular() &&
			cur.Size() == files[rel].Size() && cur.ModTime().Equal(files[rel].ModTime()) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(src, rel), target, files[rel].ModTime()); err != nil {
			return err
		}
	}
	return nil
}

// copyFile writes src to a temporary file next to dst, sets its modification time to mtime, and
// renames it to dst.
func copyFile(src, dst string, mtime time.Time) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), ".tifa-copy-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(out.Name())
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = os.Chmod(out.Name(), 0o644); err != nil {
		return err
	}
	if err = os.Chtimes(out.Name(), mtime, mtime); err != nil {
		return err
	}
	return os.Rename(out.Name(), dst)
}

// distID hashes the relative name and content of every file under dist in sorted order, except
// those skip reports (skip may be nil), each prefixed with its length, and returns the first 16
// hex digits.
func distID(dist string, skip func(name string) bool) (string, error) {
	var names []string
	err := filepath.WalkDir(dist, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, err := filepath.Rel(dist, path)
			if name := filepath.ToSlash(rel); skip == nil || !skip(name) {
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

// routeID turns the manifest key of a page.tsx, layout.tsx or layout_.tsx into the ID of its
// route or Layout.
func routeID(key string) string {
	if dir, ok := strings.CutSuffix(key, "/page.tsx"); ok {
		return dir + "/route"
	}
	return path.Dir(key) + "/layout"
}
