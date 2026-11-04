package bundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/liustack/tifa/internal/content"
)

// DevBase is where Vite's dev server serves modules under tifa dev, base in js/vite/plugin.js.
// tifa dev sends the requests under it to Vite and the others to the application.
const DevBase = "/__tifa/vite/"

// Dev builds the front end of an application for tifa dev: Vite's dev server serves the browser
// modules and updates them in open pages, and only the SSR bundle is built into .tifa/dist.
// A Dev is used by one goroutine at a time, except for PollFiles.
//
// The dev server, the SSR builds and the export reader share one Node process. The browser
// modules use the development build of React and QuickJS runs the production one, and Vite reads
// NODE_ENV once per thread, so the driver builds the SSR bundle on a worker thread of its own.
type Dev struct {
	node   *Node
	root   string
	dist   string
	port   int    // where Vite's dev server listens on 127.0.0.1
	head   string // the tags pages load the browser modules with, set once the dev server runs
	styles string // the tags that link the stylesheets of the last SSR bundle

	// converted holds the entries of the last conversion that worked, by the file of their body,
	// and headings what the plugin found in each. Content compares with them.
	converted map[string]content.Markdown
	headings  map[string][]content.Heading

	mu   sync.Mutex
	poll bool // PollFiles came before the dev server ran
}

// NewDev prepares the dev build of the application at root, with Vite's dev server on
// 127.0.0.1:port once the application has pages. output receives what Node and Vite print.
// Nothing runs before the first call that needs Node.
//
// Vite's dev server watches the application root and the files outside it that it loads, such
// as a package of the workspace. notices, when not nil, hears of each of those outside files that
// changes, and of each restart of the dev server, from another goroutine: tifa dev watches only
// the root.
func NewDev(root string, port int, output io.Writer, notices Notices) (*Dev, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	n := NewNode(root, output)
	n.mode = "development"
	n.notices = notices
	return &Dev{
		node: n,
		root: root, dist: filepath.Join(root, ".tifa", "dist"), port: port,
	}, nil
}

// Exports reads exports like Node.Exports.
func (d *Dev) Exports(files []string) (map[string][]string, error) { return d.node.Exports(files) }

// PollFiles makes Vite's dev server poll the files instead of watching them with system events,
// for when the system runs short of watches or file descriptors. A dev server that runs
// restarts, open pages reload. Called before the dev server runs, it starts that way. A watcher
// that uses FSEvents (macOS) keeps doing so: it takes no watch or descriptor per file. polling
// reports whether the dev server polls now. PollFiles may run while another call of Dev does.
func (d *Dev) PollFiles() (polling bool, err error) {
	d.mu.Lock()
	running := d.head != ""
	if !running {
		d.poll = true
	}
	d.mu.Unlock()
	if !running {
		return false, nil
	}
	var result struct {
		Polling bool `json:"polling"`
	}
	err = d.node.call("pollFiles", nil, &result)
	return result.Polling, err
}

// Close stops Vite's dev server and its Node process.
func (d *Dev) Close() error { return d.node.Close() }

// Build brings .tifa/dist up to date and returns the build ID. The first Build of an application
// with pages starts Vite's dev server. pages is false for an application without page.tsx,
// which gets no SSR bundle.
//
// The head links the stylesheets the SSR bundle imports, before the tags of the dev server, like
// the head of tifa build. Which stylesheets those are is known once the bundle is built, so a
// build that imports other stylesheets than the last one is built again with the new links.
//
// The build ID covers the public files and the script tags of the dev server, not the SSR bundle
// or the stylesheets: a change to page code reaches open pages through Vite and leaves them on
// the same build, so their navigation data requests are not refused.
//
// Build leaves the content alone: Content converts it.
//
// When ctx ends, Build closes the Dev, which ends a build that never would, so that tifa dev can
// stop.
func (d *Dev) Build(ctx context.Context, public string, pages bool) (string, error) {
	stop := context.AfterFunc(ctx, func() { d.Close() })
	defer stop()
	if err := os.MkdirAll(d.dist, 0o755); err != nil {
		return "", err
	}
	ssr := filepath.Join(d.dist, "ssr.js")
	var b Build
	if pages {
		d.mu.Lock()
		if d.head == "" {
			var result struct {
				Head string `json:"head"`
			}
			if err := d.node.call("serve", map[string]any{"port": d.port, "pollFiles": d.poll}, &result); err != nil {
				d.mu.Unlock()
				return "", fmt.Errorf("starting Vite's dev server: %w", err)
			}
			entry := DevBase + clientEntry
			d.head = result.Head + `<script type="module" src="` + html.EscapeString(entry) + `"></script>`
		}
		d.mu.Unlock()
		for {
			var result struct {
				Styles string `json:"styles"`
			}
			// Unminified, so that errors in the server render point at readable code.
			if err := d.node.call("ssr", map[string]any{"assets": d.styles + d.head, "minify": false}, &result); err != nil {
				return "", err
			}
			if result.Styles == d.styles {
				break
			}
			d.styles = result.Styles
		}
		b.Head = d.styles + d.head
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
	// Only the public files count: the SSR bundle, the content and its files change under open pages.
	files, err := distID(d.dist, func(name string) bool { return !strings.HasPrefix(name, "public/") })
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strconv.Quote(files) + d.head))
	b.ID = hex.EncodeToString(sum[:])[:16]
	return b.ID, writeBuild(d.dist, b)
}

// Content brings the content in .tifa/dist up to date with c, the entries of a scan of content/,
// nil for an app without collections: it converts the entries that need it into
// .tifa/dist/content/ and writes .tifa/dist/content.json. It returns how many entries it
// converted.
//
// An entry needs converting when it is new, or its Markdown, its front matter or its place changed
// since the last conversion that worked, or a file changed in its folder or in the folder of the
// version in the default language it falls back to. changed lists the files under content/ that
// changed, relative to the root with forward slashes. With all every entry is converted, as after
// a change of the Vite config or of the plugins it imports, and the bodies of entries that are
// gone are removed. Otherwise those stay in .tifa/dist/content/, where nothing reads them.
//
// With pages the plugin of Vite's dev server converts, so Build must have started it, and the
// bodies load their files from the dev server. Without pages the plugin of the Vite config
// converts, and the files go into .tifa/dist/assets.
//
// A failure, such as an error a plugin throws, writes no body and leaves content.json as it was.
// When ctx ends, Content closes the Dev, as Build does.
func (d *Dev) Content(ctx context.Context, pages bool, c *Content, all bool, changed []string) (int, error) {
	stop := context.AfterFunc(ctx, func() { d.Close() })
	defer stop()
	if c == nil {
		d.converted, d.headings = nil, nil
		if err := os.RemoveAll(filepath.Join(d.root, ".tifa", "content")); err != nil {
			return 0, err
		}
		return 0, finishContent(d.root, d.dist, nil)
	}
	if d.converted == nil {
		all = true
	}
	// A changed file that is no entry is a file of the entries near it, such as an image.
	sources := map[string]bool{}
	for _, m := range d.converted {
		sources[m.Source] = true
	}
	for _, m := range c.Markdown {
		sources[m.Source] = true
	}
	var files []string
	for _, f := range changed {
		if !sources[f] {
			files = append(files, f)
		}
	}
	var todo []content.Markdown
	for _, m := range c.Markdown {
		old, ok := d.converted[m.Out]
		if all || !ok || !sameMarkdown(old, m) || holdsAny(path.Dir(m.Source), files) || (m.Fallback != "" && holdsAny(m.Fallback, files)) {
			todo = append(todo, m)
		}
	}
	if err := writeEntries(d.root, todo); err != nil {
		return 0, err
	}
	headings := map[string][]content.Heading{}
	switch {
	case len(todo) > 0:
		req := map[string]any{"partial": !all}
		if !pages {
			if err := checkViteFiles(d.root); err != nil {
				return 0, err
			}
			req["alone"], req["command"] = true, "tifa dev"
		}
		if err := d.node.call("content", req, nil); err != nil {
			return 0, err
		}
		var err error
		if headings, err = readHeadings(d.root); err != nil {
			return 0, err
		}
	case all:
		if err := os.RemoveAll(filepath.Join(d.dist, "content")); err != nil {
			return 0, err
		}
	}
	converted := make(map[string]content.Markdown, len(c.Markdown))
	known := make(map[string][]content.Heading, len(c.Markdown))
	for _, m := range c.Markdown {
		converted[m.Out] = m
		if h, ok := headings[m.Out]; ok {
			known[m.Out] = h
		} else {
			known[m.Out] = d.headings[m.Out]
		}
	}
	if err := writeContentFile(d.dist, c.File, known); err != nil {
		return 0, err
	}
	d.converted, d.headings = converted, known
	return len(todo), nil
}

// sameMarkdown reports whether a and b turn into the same body.
func sameMarkdown(a, b content.Markdown) bool {
	return a.Collection == b.Collection && a.Lang == b.Lang && a.ID == b.ID && a.Source == b.Source &&
		a.Fallback == b.Fallback && a.Out == b.Out && a.Text == b.Text && bytes.Equal(a.Frontmatter, b.Frontmatter)
}

// holdsAny reports whether one of files is in the folder dir or below it.
func holdsAny(dir string, files []string) bool {
	for _, f := range files {
		if strings.HasPrefix(f, dir+"/") {
			return true
		}
	}
	return false
}
