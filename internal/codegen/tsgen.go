package codegen

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/types"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/js"
)

// rootID is the TanStack ID of the root route.
const rootID = "__root__"

// tsNode is a page, a Layout with pages in scope, or the framework's root. Each becomes one to
// three TanStack routes (see tanstackRoutes).
type tsNode struct {
	id       string // manifest ID, empty for the framework's root
	dir      string // the directory under the application root
	file     string // "route" for a page, "layout" for a Layout
	tsx      bool
	replace  bool // a Layout in layout_.tsx
	exports  []string
	load     *analyze.Load
	handlers []analyze.Handler // the method handlers of a route, which get handler functions
	segs     []pathSeg         // ServeMux segments of the full path
	parent   *tsNode
	children []*tsNode
	route    *tsRoute // the route with the component and loader, which getRouteApi names
}

// tsRoute is one route of the generated TanStack route tree.
type tsRoute struct {
	variable string
	id       string // the id option of a pathless route
	path     string // the path option, relative to the parent route
	routeID  string // the TanStack route ID
	opts     []string
	// unmatched is the Layout of the index route added below a Layout without a page at its own
	// URL, or the root Layout (the framework's root without one) of the route added for the rest
	// of a URL that no route below it starts. Go matches no route at these URLs.
	unmatched *tsNode
	// rest marks the route for the rest of the URL below the root Layout.
	rest     bool
	parent   *tsRoute
	children []*tsRoute
}

// tsxFile is the name of the component file of the node: page.tsx for a page, layout.tsx or
// layout_.tsx for a Layout.
func (n *tsNode) tsxFile() string {
	switch {
	case n.file == "layout" && n.replace:
		return "layout_.tsx"
	case n.file == "layout":
		return "layout.tsx"
	}
	return "page.tsx"
}

func (n *tsNode) exported(name string) bool { return slices.Contains(n.exports, name) }

// viteSettings is what .tifa/vite.js passes to the tifa plugin: build.target as Vite's target
// and build.i18next_cache.
type viteSettings struct {
	target       []string
	i18nextCache bool
}

// publicPaths lists the URL paths of the regular files under the public folder of the build,
// sorted. tifa build copies the folder to dist/public as it is, and the server answers these
// paths before it routes, so the router must not take them for pages.
func publicPaths(root, public string) ([]string, error) {
	dir := filepath.Join(root, public)
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && p == dir {
			return filepath.SkipAll
		}
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		paths = append(paths, "/"+filepath.ToSlash(rel))
		return nil
	})
	slices.Sort(paths)
	return paths, err
}

// js returns the settings as the object literal vite.js passes. It names i18nextCache only when
// it is on, which it is by default. Without it the plugin leaves i18next without the cache.
func (s viteSettings) js() (string, error) {
	target, err := json.Marshal(s.target)
	if err != nil {
		return "", err
	}
	if s.i18nextCache {
		return "{ target: " + string(target) + ", i18nextCache: true }", nil
	}
	return "{ target: " + string(target) + " }", nil
}

// generateTS writes the route tree, a route.gen module for every directory with a page, a Layout
// or a method handler, the two entries, the tsconfig and the runtime.
// An application without pages has no TypeScript and gets none, apart from the Vite plugin when it
// has content collections, whose Markdown the plugin turns into HTML.
func generateTS(root string, m *analyze.Manifest, dev bool, exportsOf Exports, vite viteSettings, public []string) (map[string][]byte, error) {
	if !slices.ContainsFunc(m.Routes, func(r analyze.Route) bool { return r.Page }) {
		if m.Content == nil {
			return nil, nil
		}
		files := map[string][]byte{}
		return files, viteFiles(files, vite)
	}
	_, nodes, err := buildTree(root, m, exportsOf)
	if err != nil {
		return nil, err
	}
	routes, err := routesModule(nodes, public)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"routes.tsx": routes}
	// A directory has one module for its page, its Layout and its method handlers. An API route
	// is no route of the tree: its part of the module holds only the handler functions.
	modules := map[string]*routeGen{}
	moduleOf := func(dir string, segs []pathSeg) *routeGen {
		if modules[dir] == nil {
			modules[dir] = &routeGen{dir: dir, segs: segs}
		}
		return modules[dir]
	}
	for _, n := range nodes {
		switch {
		case n.id == "":
		case n.file == "layout":
			moduleOf(n.dir, n.segs).layout = n
		default:
			g := moduleOf(n.dir, n.segs)
			g.page, g.handlers = n, n.handlers
		}
	}
	for _, r := range m.Routes {
		if !r.Page && len(r.Handlers) > 0 {
			moduleOf(r.Dir, segments(r.Path)).handlers = r.Handlers
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(modules)) {
		module, err := routeModule(modules[dir])
		if err != nil {
			return nil, err
		}
		files[dir+"/route.gen.ts"] = module
	}
	session, err := sessionModule(m.Session)
	if err != nil {
		return nil, err
	}
	files["session.gen.ts"] = session
	files["server.ts"] = []byte(tsHeader + `import "./runtime/host"
import { serve } from "./runtime/server"
import { htmlAttributes, routeTree } from "./routes"

serve(routeTree, htmlAttributes)
`)
	client := tsHeader + `import { hydrate } from "./runtime/browser"
import { htmlAttributes, routeTree } from "./routes"
`
	if dev {
		client += `import "./runtime/dev-client.js"` + "\n"
		files["runtime/dev-client.js"] = []byte(js.DevClient)
	}
	files["client.ts"] = []byte(client + "\nhydrate(routeTree, htmlAttributes)\n")
	files["tsconfig.json"] = []byte(tsconfig)
	if err := viteFiles(files, vite); err != nil {
		return nil, err
	}
	err = fs.WalkDir(js.Runtime, "runtime", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(js.Runtime, name)
		files[name] = data
		return err
	})
	return files, err
}

// viteFiles adds the Vite plugin of tifa to files, which vite.config.ts imports from .tifa/vite.js.
func viteFiles(files map[string][]byte, vite viteSettings) error {
	files["vite/plugin.js"] = []byte(js.VitePlugin)
	settingsJS, err := vite.js()
	if err != nil {
		return err
	}
	files["vite.js"] = []byte(tsHeader + `import { tifa } from "./vite/plugin.js"

// The settings of tifa.yaml the plugins use.
const settings = ` + settingsJS + `

// The Vite plugins of tifa, for the plugins of vite.config.ts.
export default function (options = {}) {
  return tifa({ ...settings, markdown: options.markdown })
}
`)
	files["vite.d.ts"] = []byte(tsHeader + `import type { PluginOption } from "vite"

export interface Options {
  // How tifa turns the Markdown of content collections into HTML. Each list is written as unified
  // takes it: a plugin, or [plugin, options].
  markdown?: {
    // Run on the Markdown, after remark-gfm.
    remarkPlugins?: readonly unknown[]
    // Run on the HTML, after tifa gave the headings their ids.
    rehypePlugins?: readonly unknown[]
  }
}

// The Vite plugins of tifa, for the plugins of vite.config.ts.
export default function tifa(options?: Options): PluginOption
`)
	return nil
}

const tsHeader = "// Code generated by tifa. DO NOT EDIT.\n"

// tsconfig makes TypeScript see each generated .tifa/app/<dir>/route.gen.ts as if it were next
// to the TSX of that directory (rootDirs). It sets no paths and no include: the
// application's own tsconfig.json, which extends this one, declares tifa/client and @/, so that
// tools that read it alone, such as the shadcn CLI, find @/ even before the first build, and no
// generated file can stand in for an application file. Its include adds .tifa, which TypeScript's
// patterns skip as a directory whose name starts with a dot. Vite resolves @/ and tifa/client by
// these paths too, for the files the include of the application's tsconfig.json matches. An
// include here would be inherited, and Vite reads it relative to the application root.
const tsconfig = `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "strict": true,
    "noEmit": true,
    "allowImportingTsExtensions": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "types": ["vite/client"],
    "rootDirs": ["..", "."]
  }
}
`

// buildTree places pages and the Layouts that have pages in scope under the framework's root, a
// Layout under its nearest ancestor Layout in the tree and a page under its nearest Layout. A
// Layout in layout_.tsx has no parent Layout, so it goes right under the framework's root.
func buildTree(root string, m *analyze.Manifest, exportsOf Exports) (*tsNode, []*tsNode, error) {
	pages := map[string]bool{}
	for _, r := range m.Routes {
		pages[r.ID] = r.Page
	}
	top := &tsNode{}
	nodes := []*tsNode{top}
	byID := map[string]*tsNode{}
	nearest := func(ids []string) *tsNode {
		for _, id := range slices.Backward(ids) {
			if n, ok := byID[id]; ok {
				return n
			}
		}
		return top
	}
	// m.Layouts lists a parent before its children.
	for _, l := range m.Layouts {
		if !slices.ContainsFunc(l.Routes, func(id string) bool { return pages[id] }) {
			continue
		}
		var chain []string
		for p := l.Parent; p != ""; p = parentOf(m, p) {
			chain = append([]string{p}, chain...)
		}
		n := &tsNode{id: l.ID, dir: l.Dir, file: "layout", tsx: l.TSX, replace: l.Replace, load: l.Load, parent: nearest(chain), segs: segments(l.Path)}
		byID[l.ID] = n
		nodes = append(nodes, n)
	}
	for _, r := range m.Routes {
		if r.Page {
			nodes = append(nodes, &tsNode{id: r.ID, dir: r.Dir, file: "route", tsx: true, load: r.Load, handlers: r.Handlers, parent: nearest(r.Layouts), segs: segments(r.Path)})
		}
	}
	var files []string
	for _, n := range nodes {
		if n.tsx {
			files = append(files, filepath.Join(root, filepath.FromSlash(n.dir), n.tsxFile()))
		}
	}
	names, err := exportsOf(files)
	if err != nil {
		return nil, nil, err
	}
	for _, n := range nodes {
		if n.tsx {
			n.exports = names[filepath.Join(root, filepath.FromSlash(n.dir), n.tsxFile())]
			if !slices.Contains(n.exports, "default") {
				return nil, nil, fmt.Errorf("%s/%s: export the component as default", n.dir, n.tsxFile())
			}
		}
		if n.parent == nil {
			continue
		}
		n.parent.children = append(n.parent.children, n)
		if len(n.segs) < len(n.parent.segs) || !slices.Equal(n.segs[:len(n.parent.segs)], n.parent.segs) {
			return nil, nil, fmt.Errorf("%s: path %s does not start with its parent's %s", n.id, formatSegs(n.segs), formatSegs(n.parent.segs))
		}
	}
	return top, nodes, nil
}

// tanstackRoutes turns the nodes into TanStack routes, parents first:
//   - the framework's root is the root route. It renders the document as its shell, so a failure
//     at any level keeps the document.
//   - the root Layout, whose path is empty, is a pathless route named after its directory.
//   - any other Layout is a path route, its directory below its parent's, so the path
//     parameters are part of its match. Two siblings never share a path: each directory holds
//     one Layout, and a page in a Layout's directory is that Layout's child.
//   - a page is a route with its path relative to its Layout's, "/" when they are equal.
//
// Route IDs follow router-core: the parent's ID, a slash, and the id or path option.
func tanstackRoutes(nodes []*tsNode) ([]*tsRoute, error) {
	var routes []*tsRoute
	add := func(parent *tsRoute, id, path string) *tsRoute {
		r := &tsRoute{variable: fmt.Sprintf("r%d", len(routes)), id: id, path: path, parent: parent}
		if parent == nil {
			r.variable, r.routeID = "root", rootID
		} else {
			parentID := parent.routeID
			if parentID == rootID {
				parentID = ""
			}
			r.routeID = cleanPath(parentID + "/" + cmp.Or(id, path))
			parent.children = append(parent.children, r)
		}
		routes = append(routes, r)
		return r
	}
	paths := map[*tsNode]string{}
	for _, n := range nodes {
		if n.parent == nil {
			continue
		}
		rel := make([]string, 0, len(n.segs))
		for _, seg := range n.segs[len(n.parent.segs):] {
			s, err := routerSegment(seg)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", n.id, err)
			}
			rel = append(rel, s)
		}
		paths[n] = strings.Join(rel, "/")
		if n.file == "route" {
			paths[n] = cmp.Or(paths[n], "/")
		}
	}
	for _, n := range nodes {
		if n.parent == nil {
			n.route = add(nil, "", "")
			continue
		}
		path := paths[n]
		parent := n.parent.route
		switch {
		case n.file == "route":
			n.route = add(parent, "", path)
		case path == "":
			n.route = add(parent, layoutRouteID(n.dir), "")
		default:
			n.route = add(parent, "", path)
		}
	}
	// Go answers a URL no route matches with a 404 inside the deepest Layout whose path starts
	// it, and the router shows the notFoundComponent of that Layout in its Outlet, as it shows
	// that of the deepest Layout it matches in part. Two kinds of URL need a route of their own:
	//   - a path route matches its own URL even when no child does, and would show the Layout
	//     with nothing in its Outlet. An index route below such a Layout fails with a 404 there.
	//     A page in the Layout's directory takes the URL, and so does a page for the rest of the
	//     path right below it, with an empty rest, also through a Layout of the rest of the path,
	//     which always has that page.
	//   - a URL that no route below the root Layout starts matches the root route alone, without
	//     the root Layout. A route for the rest of the URL below the root Layout fails with a 404
	//     for such a URL, and only for such a URL: its params.parse turns down any other, which
	//     the router then matches in part. Without a root Layout the framework's root holds it.
	//     A page for the rest of the path in app/ takes every URL instead.
	ownsURL := func(c *tsNode) bool { return paths[c] == "$" || c.file == "route" && paths[c] == "/" }
	host := nodes[0]
	for _, n := range nodes {
		if n.id == rootLayout {
			host = n
		}
	}
	for _, n := range nodes {
		switch {
		case n == host:
			if !slices.ContainsFunc(n.children, func(c *tsNode) bool { return paths[c] == "$" }) {
				r := add(n.route, "", "$")
				r.unmatched, r.rest = n, true
			}
		case n.file == "layout" && !slices.ContainsFunc(n.children, ownsURL):
			add(n.route, "", "/").unmatched = n
		}
	}
	seen := map[string]bool{}
	for _, r := range routes {
		if seen[r.routeID] {
			return nil, fmt.Errorf("codegen: two routes of the route tree get the TanStack ID %s, rename the page path or Layout directory that ends in it", r.routeID)
		}
		seen[r.routeID] = true
	}
	return routes, nil
}

// layoutRouteID is the id option of a Layout's pathless route: an underscore and its directory
// under app, dots for slashes, "_app" for the root Layout.
func layoutRouteID(dir string) string {
	if dir == "app" {
		return "_app"
	}
	return "_" + strings.ReplaceAll(strings.TrimPrefix(dir, "app/"), "/", ".")
}

// rootLayout is the manifest ID of the Layout in app/.
const rootLayout = "app/layout"

func parentOf(m *analyze.Manifest, id string) string {
	for _, l := range m.Layouts {
		if l.ID == id {
			return l.Parent
		}
	}
	panic("codegen: unknown Layout " + id)
}

// pathSeg is one segment of a ServeMux path: a literal, unescaped the way ServeMux does so that
// /%61 and /a are the same path, or a parameter, {name} or {name...}. A literal stays a literal
// whatever it unescapes to, /%7Bx%7D is not the parameter {x}.
type pathSeg struct {
	text  string // the unescaped literal, or the parameter as written
	param bool
}

// segments splits a ServeMux path into its segments, none for "/".
func segments(p string) []pathSeg {
	if p == "/" {
		return nil
	}
	var segs []pathSeg
	for _, s := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		if strings.HasPrefix(s, "{") {
			segs = append(segs, pathSeg{text: s, param: true})
			continue
		}
		// ServeMux leaves a literal with an invalid escape as it is.
		if u, err := url.PathUnescape(s); err == nil {
			s = u
		}
		segs = append(segs, pathSeg{text: s})
	}
	return segs
}

func formatSegs(segs []pathSeg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString("/" + s.text)
	}
	return cmp.Or(b.String(), "/")
}

// routerSegment turns a ServeMux segment into TanStack syntax: {name} is $name and {name...} is
// $. A literal is written the way router-core spells the matching segment of a decoded URL: it
// leaves %25 and %5C escaped and escapes control characters and "<>` again, so these are
// written escaped and everything else as it is. The reserved characters of a URL stay escaped in
// the router's path when the request escapes them, while ServeMux unescapes them, and { } $ are
// the router's own path syntax, so a literal holding one of them is refused. The analysis
// refuses such a Path with its position first.
func routerSegment(s pathSeg) (string, error) {
	if s.param {
		if strings.HasSuffix(s.text, "...}") {
			return "$", nil
		}
		return "$" + s.text[1:len(s.text)-1], nil
	}
	var b strings.Builder
	for _, r := range s.text {
		switch {
		case strings.ContainsRune("{}$;/?:@&=+,#", r):
			return "", fmt.Errorf("path segment %q holds %q, which the browser router cannot match the way ServeMux does", s.text, r)
		case r == '%' || r == '\\' || r < 0x20 || r == 0x7f || strings.ContainsRune("\"<>`", r):
			fmt.Fprintf(&b, "%%%02X", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), nil
}

// paramKey is the TanStack name of a ServeMux parameter segment, empty for a literal.
func paramKey(s pathSeg) string {
	switch {
	case !s.param:
		return ""
	case strings.HasSuffix(s.text, "...}"):
		return "_splat"
	}
	return s.text[1 : len(s.text)-1]
}

// readParams returns the TanStack keys of the path parameters that the Load of n reads, itself
// or through the Layouts it uses with tifa.Use, in path order. The component remounts when they
// change. A parameter in the path that no Load reads changes neither the data nor the state.
func readParams(n *tsNode, byID map[string]*tsNode) []string {
	read := map[string]bool{}
	var collect func(l *analyze.Load)
	collect = func(l *analyze.Load) {
		if l == nil {
			return
		}
		for _, p := range l.Params {
			read[p] = true
		}
		for _, id := range l.Uses {
			collect(byID[id].load)
		}
	}
	collect(n.load)
	var keys []string
	for _, s := range n.segs {
		if k := paramKey(s); k != "" && read[strings.TrimSuffix(strings.Trim(s.text, "{}"), "...")] {
			keys = append(keys, k)
		}
	}
	return keys
}

func params(segs []pathSeg) []string {
	var keys []string
	for _, s := range segs {
		if k := paramKey(s); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// cleanPath is cleanPath of TanStack's router-core: runs of slashes become one.
func cleanPath(p string) string {
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err) // a string always marshals
	}
	return string(b)
}

func jsStrings(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = jsString(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// routesModule writes routes.tsx: the routes of tanstackRoutes, the tree, and the router type
// registration that gives getRouteApi its types. A TSX module is loaded lazily, in its own
// browser chunk, unless it is the root Layout's, whose htmlAttributes and documentHead the root
// route and the entries need, or exports head, which the server render then calls from the
// module itself. The browser build loads these head modules lazily too: lazyRoutes in
// js/vite/plugin.js hands routes.tsx a stand-in for each, so only the root Layout is in the
// entry script.
func routesModule(nodes []*tsNode, public []string) ([]byte, error) {
	routes, err := tanstackRoutes(nodes)
	if err != nil {
		return nil, err
	}
	var imports strings.Builder
	runtime := map[string]bool{"createAppRouter": true}
	lazy := false
	eager := func(n *tsNode) bool { return n.id == rootLayout || n.exported("head") }
	// export returns the expression of one export of node i's module.
	export := func(i int, n *tsNode, name string) string {
		if eager(n) {
			return fmt.Sprintf("m%d.%s", i, name)
		}
		lazy = true
		if name == "default" {
			return fmt.Sprintf("lazyRouteComponent(m%d)", i)
		}
		return fmt.Sprintf("lazyRouteComponent(m%d, %s)", i, jsString(name))
	}
	var app *tsNode
	appIndex := 0
	for i, n := range nodes {
		if n.id == rootLayout {
			app, appIndex = n, i
		}
		if n.tsx {
			module := jsString(path.Join("..", n.dir, strings.TrimSuffix(n.tsxFile(), ".tsx")))
			if eager(n) {
				fmt.Fprintf(&imports, "import * as m%d from %s\n", i, module)
			} else {
				fmt.Fprintf(&imports, "const m%d = () => import(%s)\n", i, module)
			}
		}
		if n.load != nil {
			data := "PageData"
			if n.file == "layout" {
				data = "LayoutData"
			}
			names := fmt.Sprintf("%s as D%d", data, i)
			if hasSearch(n) {
				names += fmt.Sprintf(", %s as S%d", strings.Replace(data, "Data", "Search", 1), i)
			}
			fmt.Fprintf(&imports, "import type { %s } from %s\n", names, jsString("./"+n.dir+"/route.gen"))
		}
	}
	if app != nil && app.exported("Document") {
		return nil, errors.New("app/layout.tsx: the root Layout exports Document, which tifa no longer renders: export htmlAttributes for the attributes of <html>, and return the head tags from head")
	}
	for _, n := range nodes {
		if n.exported("NotFound") {
			return nil, fmt.Errorf("%s/%s: exports NotFound, which tifa no longer shows: a page that is not found shows the first Error from its route up, so export Error and check error instanceof RouteError && error.status === 404", n.dir, n.tsxFile())
		}
	}

	byID := map[string]*tsNode{}
	for _, n := range nodes {
		byID[n.id] = n
	}
	index := make(map[*tsNode]int, len(nodes))
	for i, n := range nodes {
		index[n] = i
	}
	runtime["DefaultError"] = true
	heads := false
	// errorOf names the Error export of a node once, so a Layout and the routes that borrow
	// its Error share one component.
	var shared strings.Builder
	errorVars := map[*tsNode]string{}
	errorNodes := map[string]*tsNode{}
	errorOf := func(n *tsNode) string {
		v, ok := errorVars[n]
		if !ok {
			v = fmt.Sprintf("e%d", index[n])
			errorVars[n], errorNodes[v] = v, n
			fmt.Fprintf(&shared, "const %s = %s\n", v, export(index[n], n, "Error"))
		}
		return v
	}
	// errorComponent is the options of a route that shows the Error e. The browser build hands
	// routes.tsx a lazy stand-in for each module it imports statically, but the root Layout's
	// (lazyRoutes in js/vite/plugin.js), and TanStack wraps a route whose errorComponent can preload
	// in Suspense. The SSR bundle has the component itself, without Suspense, so such a route turns
	// Suspense off, or the browser would hydrate another tree than the server rendered. The router
	// loads the chunk of the Error before it renders the route, as it loads the route's component.
	errorComponent := func(e string) []string {
		if n := errorNodes[e]; n != nil && n.id != rootLayout && eager(n) {
			return []string{"errorComponent: " + e, "wrapInSuspense: false"}
		}
		return []string{"errorComponent: " + e}
	}
	// errorFrom names the first Error going up from n, n included, or the framework's.
	errorFrom := func(n *tsNode) string {
		for a := n; a.parent != nil; a = a.parent {
			if a.exported("Error") {
				return errorOf(a)
			}
		}
		return "DefaultError"
	}
	// notFoundOf names the component that shows the 404 of a URL that matches nothing below n:
	// errorFrom(n) with a RouteError of status 404, made once for each Error, n0 for the
	// framework's.
	notFoundVars := map[string]string{}
	notFoundOf := func(n *tsNode) string {
		e := errorFrom(n)
		v, ok := notFoundVars[e]
		if !ok {
			runtime["missing"] = true
			v = "n" + strings.TrimPrefix(e, "e")
			if e == "DefaultError" {
				v = "n0"
			}
			notFoundVars[e] = v
			fmt.Fprintf(&shared, "const %s = missing(%s)\n", v, e)
		}
		return v
	}
	for i, n := range nodes {
		if n.parent == nil {
			n.route.opts = append(n.route.opts,
				"errorComponent: DefaultError",
				"notFoundComponent: "+notFoundOf(n))
			// The public files, which Go answers before it routes. A navigation to one, or to a path
			// under /assets/ or /__tifa/, loads the document, see leave in router.tsx.
			runtime["leave"] = true
			n.route.opts = append(n.route.opts,
				"beforeLoad: leave(publicPaths)",
				"staticData: { publicPaths }")
			// The root Layout's documentHead is the head of every page, a URL no route matches
			// and a failed root Load included.
			if app != nil && app.exported("documentHead") {
				heads = true
				n.route.opts = append(n.route.opts, fmt.Sprintf("head: routeHead(() => m%d.documentHead)", appIndex))
			}
			continue
		}
		var opts []string
		if n.tsx {
			opts = append(opts, "component: "+export(i, n, "default"))
		}
		// routeHead passes the htmlAttributes a head returns on to the matches, see head.ts.
		if n.exported("head") {
			heads = true
			opts = append(opts, fmt.Sprintf("head: routeHead(m%d.head)", i))
		}
		// Every route names the Error component it shows in its place when it fails: a page the
		// first Error from its own up, a Layout the first from the level above, since a Layout's
		// Error shows in its Outlet, inside the Layout. The server renders the component of the
		// failed route itself while the browser catches with the boundary of that route, so both
		// find the same one. A Layout's notFoundComponent shows a URL that matches nothing below
		// it in its Outlet, with the first Error from the Layout up: the router shows the
		// notFoundComponent of the deepest Layout it matched.
		if n.file == "layout" {
			opts = append(opts, errorComponent(errorFrom(n.parent))...)
			opts = append(opts, "notFoundComponent: "+notFoundOf(n))
		} else {
			opts = append(opts, errorComponent(errorFrom(n))...)
		}
		// The router finds the Loads of a URL from the staticData of the routes it matches, and
		// whether a URL is a page from the page marker: a route with a page.tsx. A Layout, the
		// routes tifa adds for a URL below a Layout that nothing matches, and an API route are no page.
		var static []string
		if n.file == "route" && n.tsx {
			static = append(static, "page: true")
		}
		if n.load != nil {
			static = append(static, "load: "+jsString(n.id))
		}
		staticData := "staticData: { " + strings.Join(static, ", ") + " }"
		var query []string
		// The types of the query parameters the route reads, for Link, navigate and useSearch.
		if hasSearch(n) {
			runtime["searchTypes"] = true
			opts = append(opts, fmt.Sprintf("validateSearch: searchTypes<S%d>()", i))
		}
		if n.load != nil {
			query = n.load.AllQuery
			runtime["load"] = true
			opts = append(opts,
				fmt.Sprintf("loader: ({ context, location, preload }) => load<D%d>(context, %s, location, preload)", i, jsString(n.id)),
				staticData)
			if len(query) > 0 {
				runtime["pick"] = true
				opts = append(opts, fmt.Sprintf("loaderDeps: ({ search }) => pick(search, %s)", jsStrings(query)))
			}
		} else if len(static) > 0 {
			opts = append(opts, staticData)
		}
		var args, deps []string
		if keys := readParams(n, byID); len(keys) > 0 {
			args = append(args, "params")
			for _, k := range keys {
				deps = append(deps, "params."+k)
			}
		}
		if len(query) > 0 {
			args = append(args, "search")
			deps = append(deps, fmt.Sprintf("pick(search, %s)", jsStrings(query)))
		}
		if len(deps) > 0 {
			opts = append(opts, fmt.Sprintf("remountDeps: ({ %s }) => [%s]", strings.Join(args, ", "), strings.Join(deps, ", ")))
		}
		n.route.opts = append(n.route.opts, opts...)
	}

	for _, r := range routes {
		if r.unmatched == nil {
			continue
		}
		runtime["unmatched"] = true
		if r.rest {
			runtime["outside"] = true
			r.opts = append(r.opts, fmt.Sprintf("params: { parse: outside(%s) }", routePrefixes(nodes)))
		}
		r.opts = append(r.opts, errorComponent(errorFrom(r.unmatched))...)
		r.opts = append(r.opts, "loader: unmatched")
	}

	var body strings.Builder
	fmt.Fprintf(&body, "const publicPaths: string[] = %s\n\n", jsStrings(public))
	if shared.Len() > 0 {
		body.WriteString(shared.String() + "\n")
	}
	for _, r := range routes {
		var opts []string
		if r.parent == nil {
			fmt.Fprintf(&body, "const root = createRootRouteWithContext<RouterContext>()({\n")
		} else {
			fmt.Fprintf(&body, "const %s = createRoute({\n", r.variable)
			opts = append(opts, "getParentRoute: () => "+r.parent.variable)
			if r.id != "" {
				opts = append(opts, "id: "+jsString(r.id))
			} else {
				opts = append(opts, "path: "+jsString(r.path))
			}
		}
		for _, o := range append(opts, r.opts...) {
			body.WriteString("  " + o + ",\n")
		}
		body.WriteString("})\n\n")
	}
	fmt.Fprintf(&body, "export const routeTree = %s\n", subtree(routes[0]))
	// The attributes of <html> every page starts from, below those the head functions of its
	// routes return. The server writes the document around the React root, the browser sets
	// them on a page rendered without SSR and after each navigation.
	if app != nil && app.exported("htmlAttributes") {
		fmt.Fprintf(&body, "export const htmlAttributes = m%d.htmlAttributes\n", appIndex)
	} else {
		body.WriteString("export const htmlAttributes = undefined\n")
	}
	body.WriteString(`
declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter<typeof routeTree>>
  }
}
`)

	var out strings.Builder
	out.WriteString(tsHeader)
	router := "createRootRouteWithContext, createRoute"
	if lazy {
		router += ", lazyRouteComponent"
	}
	fmt.Fprintf(&out, `import { %s } from "@tanstack/react-router"`+"\n", router)
	var names []string
	for _, name := range []string{"createAppRouter", "DefaultError", "leave", "load", "missing", "outside", "pick", "searchTypes", "unmatched"} {
		if runtime[name] {
			names = append(names, name)
		}
	}
	names = append(names, "type RouterContext")
	fmt.Fprintf(&out, `import { %s } from "./runtime/router"`+"\n", strings.Join(names, ", "))
	if heads {
		out.WriteString(`import { routeHead } from "./runtime/head"` + "\n")
	}
	out.WriteString(imports.String())
	out.WriteString("\n")
	out.WriteString(body.String())
	return []byte(out.String()), nil
}

// routePrefixes writes the paths a URL may start with to be matched, in full or in part, by a route
// below the root Layout, or below a layout_.tsx outside it: the path of every Layout and page but
// the root Layout and the pages in
// the directory of their Layout, which the router matches only at the Layout's URL. A path for
// the rest of the URL counts up to that rest. A literal is its text, a parameter null.
func routePrefixes(nodes []*tsNode) string {
	seen := map[string]bool{}
	var out []string
	for _, n := range nodes {
		if n.parent == nil || n.id == rootLayout || n.file == "route" && n.route.path == "/" {
			continue
		}
		var segs []string
		for _, seg := range n.segs {
			if strings.HasSuffix(seg.text, "...}") {
				break
			}
			if seg.param {
				segs = append(segs, "null")
			} else {
				segs = append(segs, jsString(seg.text))
			}
		}
		if p := "[" + strings.Join(segs, ", ") + "]"; !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return "[" + strings.Join(out, ", ") + "]"
}

func subtree(r *tsRoute) string {
	if len(r.children) == 0 {
		return r.variable
	}
	children := make([]string, len(r.children))
	for i, c := range r.children {
		children[i] = subtree(c)
	}
	return r.variable + ".addChildren([" + strings.Join(children, ", ") + "])"
}

// routeGen is what the route.gen module of one directory holds: the page and the Layout of the
// directory, as nodes of the route tree, and its method handlers.
type routeGen struct {
	dir      string
	segs     []pathSeg
	page     *tsNode // nil without page.tsx
	layout   *tsNode // nil without a Layout
	handlers []analyze.Handler
}

// routeModule writes the route.gen module of a directory: page and layout for the hooks of its
// page and Layout, PageData and LayoutData for the result of its Load and Layout functions,
// Params for its path parameters, and a function per method handler, named after its method,
// that sends a request to it.
func routeModule(g *routeGen) ([]byte, error) {
	// The data and the inputs and results of the handlers share the declarations of recursive types.
	var goTypes []types.Type
	for _, n := range []*tsNode{g.layout, g.page} {
		if n != nil && n.load != nil {
			if n.load.Result == nil {
				return nil, fmt.Errorf("codegen: %s: the manifest carries no Load result type, analyze the application in this process", n.id)
			}
			goTypes = append(goTypes, n.load.Result)
		}
	}
	for _, h := range g.handlers {
		if (h.Input != "") != (h.InputType != nil) || (h.Output != "") != (h.OutputType != nil) {
			return nil, fmt.Errorf("codegen: %s: the manifest carries no types of %s, analyze the application in this process", g.dir, h.Method)
		}
		for _, t := range []types.Type{h.InputType, h.OutputType} {
			if t != nil {
				goTypes = append(goTypes, t)
			}
		}
	}
	shapes, decls, html, err := analyze.TSShapes(goTypes)
	if err != nil {
		return nil, fmt.Errorf("codegen: %s: %w", g.dir, err)
	}
	next := func() string {
		s := shapes[0]
		shapes = shapes[1:]
		return s
	}

	var b strings.Builder
	b.WriteString(tsHeader)
	if g.page != nil || g.layout != nil {
		b.WriteString(`import { getRouteApi } from "@tanstack/react-router"` + "\n")
	}
	if len(g.handlers) > 0 {
		names := []string{"call"}
		for _, s := range g.segs {
			if key := paramKey(s); key == "_splat" {
				names = append(names, "rest")
			} else if key != "" && !slices.Contains(names, "segment") {
				names = append(names, "segment")
			}
		}
		runtime := strings.Repeat("../", strings.Count(g.dir, "/")+1) + "runtime/call"
		fmt.Fprintf(&b, "import { %s, type CallOptions } from %s\n", strings.Join(names, ", "), jsString(runtime))
	}
	if html {
		// The type of tifa.HTML, the one tifa/client exports.
		runtime := strings.Repeat("../", strings.Count(g.dir, "/")+1) + "runtime/html"
		fmt.Fprintf(&b, "import type { HTML } from %s\n", jsString(runtime))
	}
	b.WriteString("\n")
	for _, d := range decls {
		b.WriteString(d + "\n")
	}
	if len(decls) > 0 {
		b.WriteString("\n")
	}
	// data writes the type of the data of a Layout or page.
	data := func(n *tsNode, name, fn string) {
		shape := "undefined"
		if n.load != nil {
			shape = next()
			fmt.Fprintf(&b, "// %s is what the Go %s function of %s returns, as JSON.\n", name, fn, g.dir)
		} else {
			fmt.Fprintf(&b, "// %s has no Go %s function.\n", g.dir, fn)
		}
		fmt.Fprintf(&b, "export type %s = %s\n\n", name, shape)
	}
	if g.layout != nil {
		data(g.layout, "LayoutData", "Layout")
	}
	if g.page != nil {
		data(g.page, "PageData", "Load")
	}
	if g.layout != nil {
		searchType(&b, g.layout, "LayoutSearch", "Layout", g.dir)
	}
	if g.page != nil {
		searchType(&b, g.page, "PageSearch", "Load", g.dir)
	}
	keys := params(g.segs)
	paramsType := "Record<string, never>"
	if len(keys) > 0 {
		props := make([]string, len(keys))
		for i, k := range keys {
			props[i] = k + ": string"
		}
		paramsType = "{ " + strings.Join(props, "; ") + " }"
	}
	fmt.Fprintf(&b, "export type Params = %s\n", paramsType)
	if g.layout != nil {
		fmt.Fprintf(&b, "\nexport const layout = getRouteApi(%s)\n", jsString(g.layout.route.routeID))
	}
	if g.page != nil {
		fmt.Fprintf(&b, "\nexport const page = getRouteApi(%s)\n", jsString(g.page.route.routeID))
	}
	for _, h := range g.handlers {
		b.WriteString("\n")
		options := []string{"CallOptions"}
		if len(keys) > 0 {
			options = append(options, "{ params: Params }")
		}
		result, answer := "Response", "raw"
		if h.Input != "" {
			fmt.Fprintf(&b, "export type %sInput = %s\n", h.Method, next())
			options = append(options, fmt.Sprintf("{ input: %sInput }", h.Method))
			result, answer = "void", "none"
		}
		if h.Output != "" {
			fmt.Fprintf(&b, "export type %sOutput = %s\n", h.Method, next())
			// The promise of a redirected call resolves to undefined, see call in
			// js/runtime/call.ts. Whether a handler redirects is not known statically: OnRequest
			// may redirect any request, and a redirect is an error value that can come from
			// anywhere the handler's error does.
			result, answer = h.Method+"Output | undefined", "json"
		}
		if h.Input != "" || h.Output != "" {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "// %s sends a request to the Go %s handler of %s.\n", h.Method, h.Method, g.dir)
		if answer == "json" {
			b.WriteString("// It resolves to undefined when the handler or OnRequest redirects.\n")
		}
		body := ""
		if h.Form {
			b.WriteString("// Its input holds files, so it is sent as a multipart form.\n")
			body = `, "form"`
		}
		param := "options: " + strings.Join(options, " & ")
		if len(options) == 1 {
			param = "options: CallOptions = {}"
		}
		// async, so that a refused path parameter rejects the promise like any other failure.
		fmt.Fprintf(&b, "export async function %s(%s): Promise<%s> {\n", h.Method, param, result)
		fmt.Fprintf(&b, "  return call(%s, %s, options, %s%s) as Promise<%s>\n}\n", jsString(h.Method), urlTemplate(g.segs), jsString(answer), body, result)
	}
	return []byte(b.String()), nil
}

// urlTemplate writes the path of segs as a template literal that puts the path parameters of a
// handler function's options into the URL.
func urlTemplate(segs []pathSeg) string {
	if len(segs) == 0 {
		return "`/`"
	}
	var b strings.Builder
	b.WriteString("`")
	for _, s := range segs {
		b.WriteString("/")
		switch key := paramKey(s); {
		case key == "_splat":
			b.WriteString("${rest(options.params._splat)}")
		case key != "":
			b.WriteString("${segment(options.params." + key + ")}")
		default:
			b.WriteString(strings.NewReplacer("\\", "\\\\", "`", "\\`", "$", "\\$").Replace(s.text))
		}
	}
	b.WriteString("`")
	return b.String()
}

// sessionModule writes session.gen.ts: SessionData is the JSON of what the Exposed method of the
// session type returns, and never when the app has no session or the type has no such method.
func sessionModule(s *analyze.Session) ([]byte, error) {
	var b strings.Builder
	b.WriteString(tsHeader + "\n")
	b.WriteString("// SessionEnabled is true when the app calls tifa.SetSession.\n")
	fmt.Fprintf(&b, "export type SessionEnabled = %t\n\n", s != nil)
	if s == nil || !s.Exposed {
		b.WriteString("// The app has no session type with an Exposed method.\nexport type SessionData = never\n")
		return []byte(b.String()), nil
	}
	if s.ExposedResult == nil {
		return nil, fmt.Errorf("codegen: the manifest carries no Exposed result type, analyze the application in this process")
	}
	shape, decls, html, err := analyze.TSShape(s.ExposedResult)
	if err != nil {
		return nil, fmt.Errorf("codegen: session Exposed result: %w", err)
	}
	if html {
		b.WriteString("import type { HTML } from \"./runtime/html\"\n\n")
	}
	for _, d := range decls {
		b.WriteString(d + "\n")
	}
	if len(decls) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("// SessionData is what the Exposed method of the session type returns, as JSON.\n")
	fmt.Fprintf(&b, "export type SessionData = %s\n", shape)
	return []byte(b.String()), nil
}

// searchType writes the type of the query parameters the Load or Layout function of n reads, which
// routes.tsx gives its route for Link, navigate and useSearch. A value read with c.Query is a
// string, one read with c.QueryValues a string or an array of strings, as parseSearch of the
// runtime reads them. Every key may be left out, and other keys are open: the app may use
// parameters of its own in the browser, which Go does not read. A function that reads none gets no
// type.
func searchType(b *strings.Builder, n *tsNode, name, fn, dir string) {
	if !hasSearch(n) {
		return
	}
	fmt.Fprintf(b, "// %s is the query parameters the Go %s function of %s reads, as the router reads them.\n", name, fn, dir)
	b.WriteString("// Other parameters may come too, as strings.\n")
	fmt.Fprintf(b, "export type %s = {\n", name)
	for _, key := range n.load.Query {
		value := "string"
		if slices.Contains(n.load.QueryValues, key) {
			value = "string | string[]"
		}
		fmt.Fprintf(b, "  %s?: %s\n", jsString(key), value)
	}
	b.WriteString("  [key: string]: string | string[] | undefined\n}\n\n")
}

// hasSearch reports whether the Load or Layout function of n reads query parameters, so that
// route.gen exports their type and routes.tsx gives it to the route.
func hasSearch(n *tsNode) bool { return n.load != nil && len(n.load.Query) > 0 }
