package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// DevBase is where Vite's dev server serves modules under tifa dev, base in js/vite/plugin.js.
// tifa dev sends the requests under it to Vite and the others to the application.
const DevBase = "/__tifa/vite/"

// Dev builds the front end of an application for tifa dev: Vite's dev server serves the browser
// modules and updates them in open pages, and only the SSR bundle is built into .tifa/dist.
// A Dev is used by one goroutine at a time.
//
// The dev server, the SSR builds and the export reader share one Node process. The browser
// modules use the development build of React and QuickJS runs the production one, and Vite reads
// NODE_ENV once per thread, so the driver builds the SSR bundle on a worker thread of its own.
type Dev struct {
	node *Node
	root string
	dist string
	port int    // where Vite's dev server listens on 127.0.0.1
	head string // the tags pages load the browser modules with, set once the dev server runs
}

// NewDev prepares the dev build of the application at root, with Vite's dev server on
// 127.0.0.1:port once the application has pages. output receives what Node and Vite print.
// Nothing runs before the first call that needs Node.
func NewDev(root string, port int, output io.Writer) (*Dev, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Dev{
		node: NewNode(root, output),
		root: root, dist: filepath.Join(root, ".tifa", "dist"), port: port,
	}, nil
}

// Exports reads exports like Node.Exports.
func (d *Dev) Exports(files []string) (map[string][]string, error) { return d.node.Exports(files) }

// Close stops Vite's dev server and its Node process.
func (d *Dev) Close() error { return d.node.Close() }

// Build brings .tifa/dist up to date and returns the build ID. The first Build of an application
// with pages starts Vite's dev server. pages is false for an application without route.tsx,
// which gets no SSR bundle.
//
// The build ID covers the public files and the tags of the dev server, not the SSR bundle: a
// change to page code reaches open pages through Vite and leaves them on the same build, so their
// navigation data requests are not refused.
func (d *Dev) Build(public string, pages bool) (string, error) {
	if err := os.MkdirAll(d.dist, 0o755); err != nil {
		return "", err
	}
	ssr := filepath.Join(d.dist, "ssr.js")
	var b Build
	if pages {
		if d.head == "" {
			var result struct{ Head string }
			if err := d.node.call("serve", map[string]any{"port": d.port}, &result); err != nil {
				return "", fmt.Errorf("starting Vite's dev server: %w", err)
			}
			entry := DevBase + clientEntry
			d.head = result.Head + `<script type="module" src="` + html.EscapeString(entry) + `"></script>`
		}
		b.Head = d.head
		// Unminified, so that errors in the server render point at readable code.
		if err := d.node.call("ssr", map[string]any{"assets": b.Head, "minify": false}, nil); err != nil {
			return "", err
		}
	} else if err := os.Remove(ssr); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	for _, name := range []string{"public", "build.json"} {
		if err := os.RemoveAll(filepath.Join(d.dist, name)); err != nil {
			return "", err
		}
	}
	if err := copyDir(public, filepath.Join(d.dist, "public")); err != nil {
		return "", err
	}
	files, err := distID(d.dist, "ssr.js")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strconv.Quote(files) + b.Head))
	b.ID = hex.EncodeToString(sum[:])[:16]
	return b.ID, writeBuild(d.dist, b)
}
