package foreign

import (
	"regexp"
	"strings"
)

// Doc anchors, all in the docs of the Tifa version the app uses.
const (
	docFiles    = "docs/app.md#the-files-of-a-folder"
	docFolders  = "docs/app.md#urls-come-from-folders"
	docRouteGo  = "docs/routes.md#routego"
	docPage     = "docs/pages.md#pagetsx"
	docLink     = "docs/pages.md#query-parameters-in-link-and-navigate"
	docLayout   = "docs/pages.md#layout"
	docHandlers = "docs/routes.md#method-handlers-with-input-and-result"
	docCalling  = "docs/pages.md#calling-method-handlers-from-the-browser"
	docBrowser  = "docs/server-render.md#components-that-draw-only-in-the-browser"
	docCtx      = "docs/routes.md#ctx"
	docCache    = "docs/ssr-and-cache.md#cache-for-cdns"
	docSSR      = "docs/ssr-and-cache.md#ssr-and-cache"
	docErrors   = "docs/pages.md#errors-on-a-page"
	docNotFound = "docs/pages.md#404"
	docHooks    = "docs/hooks.md#hooksgo"
	docWaiting  = "docs/server-render.md#components-that-wait"
	docContent  = "docs/content.md#declaring-a-collection"
	docPrivate  = "docs/ssr-and-cache.md#user-independent-pages-and-private-data"
	docStatic   = "docs/app.md#no-static-generation"
	docImages   = "docs/app.md#images-and-fonts"
	docPublic   = "docs/app.md#static-files"
	docLoading  = "docs/app.md#loading-states"
	docHead     = "docs/pages.md#title-and-meta-tags"
)

// The Tifa way to load data, change data and link, which several rules repeat.
const (
	insteadLoad    = "Load in route.go loads the data of a page, as func Load(c *tifa.Ctx) (PageData, error), and the page reads it with page.useLoaderData() from './route.gen'"
	insteadHandler = "write a method handler in route.go, such as func POST(c *tifa.Ctx, in AddInput) (Todo, error), and call the POST function from './route.gen' in the page"
	insteadRouter  = "TanStack Router runs in the browser: import { Link, useNavigate } from '@tanstack/react-router'. The data comes from Load in route.go, read with page.useLoaderData() from './route.gen', and a redirect is return tifa.Redirect(url, status) in Go"
)

// importRule matches a module name: Module itself, or a name below Prefix + "/" . A prefix that ends
// in ":" or "-" matches by its text alone (astro:content, @tanstack/start-client-core).
type importRule struct {
	names []string // the module itself, and what is below it
	text  []string // any module that starts with this text
	Rule
}

func (r importRule) matches(spec string) bool {
	for _, n := range r.names {
		if spec == n || strings.HasPrefix(spec, n+"/") {
			return true
		}
	}
	for _, p := range r.text {
		if strings.HasPrefix(spec, p) {
			return true
		}
	}
	return false
}

// importRules are checked in order, the first match speaks. Every one is an error: the module
// of another framework is not in a Tifa app, and an import that resolves anyway brings the
// router or the server of that framework into the bundle.
var importRules = []importRule{
	{names: []string{"next/link"}, Rule: Rule{
		Says:    "{module} is a module of Next.js",
		Instead: "import { Link } from '@tanstack/react-router' and write <Link to=\"/orders\" search={{ sort: 'price' }}>",
		Doc:     docLink}},
	{names: []string{"next/navigation", "next/router"}, Rule: Rule{
		Says:    "{module} is a module of Next.js",
		Instead: insteadRouter + ". Path parameters come from page.useParams() and query parameters from page.useSearch()",
		Doc:     docLink}},
	{names: []string{"next/head", "next/document"}, Rule: Rule{
		Says:    "{module} is a module of Next.js",
		Instead: "a page exports head, such as export const head = () => ({ meta: [{ title: 'Orders' }] }), and the root layout.tsx exports htmlAttributes and documentHead for the document",
		Doc:     docLayout}},
	{names: []string{"next/image"}, Rule: Rule{
		Says:    "{module} is a module of Next.js",
		Instead: "Tifa has no image component and does not resize images: write a plain <img> with width, height and loading=\"lazy\", and put the file in public/ or reference it from CSS",
		Doc:     docImages}},
	{names: []string{"next/font"}, text: []string{"next/font/"}, Rule: Rule{
		Says:    "{module} is a module of Next.js",
		Instead: "Tifa does not load fonts for you: write @font-face in your CSS with the font file in the app",
		Doc:     docImages}},
	{names: []string{"next/dynamic"}, Rule: Rule{
		Says:    "{module} is a module of Next.js",
		Instead: "put a component that needs the browser in a *.client.tsx file and load it with clientOnly from 'tifa/client'",
		Doc:     docBrowser}},
	{names: []string{"next/headers"}, Rule: Rule{
		Says:    "{module} is a module of Next.js",
		Instead: "read cookies and headers in Go, with c.Cookie(name) and c.Header(name) in Load or a method handler",
		Doc:     docCtx}},
	{names: []string{"next/server"}, Rule: Rule{
		Says:    "{module} is a module of Next.js",
		Instead: "route handlers are method handlers in route.go (func POST(c *tifa.Ctx, in AddInput) (Todo, error)) and middleware is OnRequest in hooks.go",
		Doc:     docHandlers}},
	{names: []string{"next"}, Rule: Rule{
		Says:    "{module} is the Next.js package",
		Instead: "Tifa has no Next.js modules: " + insteadLoad + ", the head is the head export, and links come from '@tanstack/react-router'",
		Doc:     docPage}},
	{names: []string{"react-router", "react-router-dom", "@remix-run"}, text: []string{"@remix-run/"}, Rule: Rule{
		Says:    "{module} is a module of Remix or React Router",
		Instead: insteadRouter,
		Doc:     docPage}},
	{names: []string{"@tanstack/react-start", "@tanstack/start"}, text: []string{"@tanstack/react-start-", "@tanstack/start-"}, Rule: Rule{
		Says:    "{module} is a module of TanStack Start (@tanstack/react-router is fine)",
		Instead: "there are no server functions: " + insteadHandler,
		Doc:     docCalling}},
	{names: []string{"@sveltejs/kit"}, text: []string{"$app/"}, Rule: Rule{
		Says:    "{module} is a module of SvelteKit",
		Instead: "pages are React: " + insteadLoad + ", links come from '@tanstack/react-router', and a redirect is return tifa.Redirect(url, status) in Go",
		Doc:     docPage}},
	{text: []string{"astro:content"}, Rule: Rule{
		Says:    "{module} is a module of Astro",
		Instead: "declare a content collection in Go and read it in Load, see the doc",
		Doc:     docContent}},
	{text: []string{"astro:"}, Rule: Rule{
		Says:    "{module} is a module of Astro",
		Instead: "pages are React components: " + insteadLoad,
		Doc:     docPage}},
}

// matchImport returns the rule of a module name.
func matchImport(spec string) (Rule, bool) {
	for _, r := range importRules {
		if r.matches(spec) {
			r.Says = strings.ReplaceAll(r.Says, "{module}", spec)
			return r.Rule, true
		}
	}
	return Rule{}, false
}

var (
	useClient = Rule{
		Says:    "'use client' is a directive of React Server Components (Next.js, TanStack Start)",
		Instead: "it does nothing, because every component renders on the server and hydrates in the browser. A component that must not render on the server goes in a *.client.tsx file and loads with clientOnly from 'tifa/client'",
		Doc:     docBrowser, Warning: true}
	useServer = Rule{
		Says:    "'use server' marks a server action in React Server Components (Next.js, TanStack Start)",
		Instead: "Tifa has no server actions, and this function would run in the browser: " + insteadHandler,
		Doc:     docCalling}
)

// headInComponent is a <title> or <meta> in the JSX of a component. React 19 hoists it into the head
// in the browser, but Tifa writes the head of the document as a string and React renders only the
// root in <body>, so the server HTML has the tag in <body>.
func headInComponent(name string) Rule {
	return Rule{
		Says:    "<" + name + "> written in a component lands in <body>, where crawlers do not read it",
		Instead: "write the title and meta tags in the `export const head` of the page or Layout",
		Doc:     docHead, Warning: true}
}

// exportRule is the idiom of one exported name. match, when set, says which values make it the idiom of
// another framework: a name an app could use for itself speaks only for the kind of value the
// framework writes.
type exportRule struct {
	match func(Export) bool
	Rule
}

func (r exportRule) matches(e Export) bool { return r.match == nil || r.match(e) }

func kind(kinds ...string) func(Export) bool {
	return func(e Export) bool { return contains(kinds, e.Kind) }
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func warn(says, instead, doc string) Rule {
	return Rule{Says: says, Instead: instead, Doc: doc, Warning: true}
}

// exportRules are the names that page.tsx, layout.tsx and layout_.tsx export for another framework
// and Tifa never reads. A name that an app could use for itself (action, loader, metadata,
// revalidate, dynamic) speaks only for the kind of value the framework writes: a function for
// loader and action, an object for metadata, a number or false for revalidate, and one of the
// four strings of Next.js for dynamic. The names no one would pick for themselves speak always.
var exportRules = map[string]exportRule{
	"getServerSideProps": {Rule: warn("getServerSideProps is the data function of Next.js pages", insteadLoad, docRouteGo)},
	"getStaticProps": {Rule: warn("getStaticProps is the data function of Next.js static pages",
		insteadLoad+". Tifa renders on each request: the render memo skips a render when the data is the same, and const Cache = true lets a CDN keep a public page", docStatic)},
	"getStaticPaths": {Rule: warn("getStaticPaths lists the pages of a Next.js dynamic route to build",
		"there is no static build and no list of paths. A folder named name+ matches any value, and Load returns tifa.ErrNotFound for one that does not exist", docStatic)},
	"generateStaticParams": {Rule: warn("generateStaticParams lists the pages of a Next.js dynamic route to build",
		"there is no static build and no list of paths. A folder named name+ matches any value, and Load returns tifa.ErrNotFound for one that does not exist", docStatic)},
	"generateMetadata": {Rule: warn("generateMetadata is the head function of Next.js", "export head, such as export const head = ({ loaderData }) => ({ meta: [{ title: loaderData?.name }] })", docPage)},
	"metadata": {match: kind("object"), Rule: warn("an exported metadata object is how Next.js sets the head",
		"export head, such as export const head = () => ({ meta: [{ title: 'Orders' }] }), and set the defaults of every page with documentHead in the root layout.tsx", docPage)},
	"loader": {match: kind("function"), Rule: warn("an exported loader is how Remix and React Router load data", insteadLoad, docRouteGo)},
	"clientLoader": {match: kind("function"), Rule: warn("an exported clientLoader is how Remix and React Router load data in the browser",
		insteadLoad+". For data only the browser can ask for, call the GET function of an API route from './route.gen'", docPrivate)},
	"action":       {match: kind("function"), Rule: warn("an exported action is how Remix and React Router change data", insteadHandler, docHandlers)},
	"clientAction": {match: kind("function"), Rule: warn("an exported clientAction is how Remix and React Router change data in the browser", insteadHandler, docHandlers)},
	"revalidate": {match: func(e Export) bool { return e.Kind == "number" || e.Kind == "boolean" && e.Value == "false" }, Rule: warn("an exported revalidate sets how long Next.js keeps a page",
		"a CDN keeps a page that is the same for everyone when route.go has const Cache = 10 * time.Minute (or const Cache = true). The program itself runs Load on every request", docStatic)},
	"dynamic": {match: func(e Export) bool {
		return e.Kind == "string" && contains([]string{"auto", "force-dynamic", "error", "force-static"}, e.Value)
	}, Rule: warn("an exported dynamic sets how Next.js renders a page",
		"every page loads its data and renders on each request already, so remove it. A page that needs no server render has const SSR = false in route.go, and one that a CDN may keep has const Cache = true", docSSR)},
}

// pageFile reports whether the base name is a file whose exports Tifa reads: the page and the Layout.
func pageFile(base string) bool {
	return base == "page.tsx" || base == "layout.tsx" || base == "layout_.tsx"
}

// callRules are the calls, in page.tsx and layout.tsx, of functions that another framework uses to
// make a route or a server function. The function must be imported: a function of the app's own
// with the same name is none of our business.
var callRules = map[string]Rule{
	"createFileRoute": warn("createFileRoute makes a route by hand in TanStack Router (and Start)",
		"the folder is the route: app/orders/page.tsx is /orders, and './route.gen' exports its TanStack route as page, for page.useLoaderData() and page.useParams()", docPage),
	"createRootRoute": warn("createRootRoute makes the root route by hand in TanStack Router (and Start)",
		"app/layout.tsx is the root Layout, and the framework makes the root route", docLayout),
	"createServerFn": warn("createServerFn makes a server function in TanStack Start", "there are no server functions: "+insteadHandler, docCalling),
}

// Check of the files of a tree, see ScanTree.
var (
	svelteFile  = regexp.MustCompile(`^\+(page|layout|server|error)(\.server)?\.(svelte|ts|js)$`)
	remixIndex  = regexp.MustCompile(`^_index\.(tsx|jsx|ts|js)$`)
	remixFlat   = regexp.MustCompile(`^([\w-]+\.)*\$[\w-]*\.(tsx|jsx|ts|js)$`)
	nextPageExt = regexp.MustCompile(`^(page|layout)\.(jsx|js|ts|mjs)$`)
	nextRouteTS = regexp.MustCompile(`^route\.(ts|js|jsx)$`)
	nextSpecial = regexp.MustCompile(`^(loading|error|global-error|not-found)\.(tsx|jsx|ts|js)$`)
	middleware  = regexp.MustCompile(`^middleware\.(ts|js|mjs)$`)
)

// The rules of file names. Each takes the file name and returns the rule, with the name in its message.
func ruleSvelteFile(name string) Rule {
	return Rule{Says: name + " is a file of SvelteKit", Instead: "a page is page.tsx, a Layout is layout.tsx, and the Go side of the folder (Load, method handlers) is route.go. Pages are React", Doc: docFiles}
}

func ruleRemixIndex(name string) Rule {
	return Rule{Says: name + " is the index route file of Remix", Instead: "the page of a URL is page.tsx in the folder of that URL, and the home page is app/page.tsx", Doc: docFolders}
}

func ruleRemixFlat(name string) Rule {
	return warn(name+" is a flat route file of Remix", "a parameter is a folder named name+ with page.tsx inside, such as app/orders/order+/page.tsx for /orders/{order}", docFolders)
}

func ruleNextPage(name, kind string) Rule {
	return warn(name+" is the "+kind+" file of Next.js", "Tifa reads only page.tsx for a page and layout.tsx for a Layout", docFiles)
}

func ruleNextRoute(name string) Rule {
	return warn(name+" is the route handler file of Next.js", "a route handler is a method handler in route.go, such as func POST(c *tifa.Ctx, in AddInput) (Todo, error)", docHandlers)
}

func ruleMiddleware(name string) Rule {
	return warn(name+" is the middleware file of Next.js", "middleware is OnRequest in hooks.go, which runs for every page and API route", docHooks)
}

var specialFiles = map[string]struct{ instead, doc string }{
	"loading":      {"a page renders whole on the server, so there is no loading state to show. A component that waits uses Suspense", docLoading},
	"error":        {"export ErrorBoundary from page.tsx or layout.tsx, such as export function ErrorBoundary({ error }) { ... }", docErrors},
	"global-error": {"export ErrorBoundary from the root layout.tsx, such as export function ErrorBoundary({ error }) { ... }", docErrors},
	"not-found":    {"a 404 shows in the Error export of page.tsx or layout.tsx: check error instanceof TifaError && error.status === 404", docNotFound},
}

func ruleNextSpecial(name, stem string) Rule {
	s := specialFiles[stem]
	kind := stem
	if stem == "global-error" {
		kind = "error"
	}
	return warn(name+" is the "+kind+" file of Next.js", s.instead, s.doc)
}

// folderRule is a folder name of another framework, with how to write it in Tifa. The name of the
// Tifa folder is the first submatch, or none.
type folderRule struct {
	re   *regexp.Regexp
	rule func(m []string) Rule
}

var folderRules = []folderRule{
	{regexp.MustCompile(`^\[\[?\.\.\.(\w+)\]\]?$`), func(m []string) Rule {
		return Rule{Says: "the folder name " + m[0] + " is a catch-all route of Next.js", Instead: "name the folder " + m[1] + "++, which matches the rest of the path and may be empty (c.Param(\"" + m[1] + "\") in Go, _splat in TypeScript)", Doc: docFolders}
	}},
	{regexp.MustCompile(`^\[(\w+)\]$`), func(m []string) Rule {
		return Rule{Says: "the folder name " + m[0] + " is a dynamic route of Next.js and SvelteKit", Instead: "name the folder " + m[1] + "+, read with c.Param(\"" + m[1] + "\")", Doc: docFolders}
	}},
	{regexp.MustCompile(`^[$:](\w+)$`), func(m []string) Rule {
		return Rule{Says: "the folder name " + m[0] + " is a route parameter of Remix or React Router", Instead: "name the folder " + m[1] + "+, read with c.Param(\"" + m[1] + "\")", Doc: docFolders}
	}},
	{regexp.MustCompile(`^\(\w[\w-]*\)$`), func(m []string) Rule {
		return Rule{Says: "the folder name " + m[0] + " is a route group of Next.js", Instead: "there are no route groups: every folder below app/ is one URL segment, and layout.tsx wraps the pages in its folder and below", Doc: docFolders}
	}},
	{regexp.MustCompile(`^@\w+$`), func(m []string) Rule {
		return Rule{Says: "the folder name " + m[0] + " is a parallel route slot of Next.js", Instead: "there are no parallel routes: every folder below app/ is one URL segment, and a part of a page is a component", Doc: docFolders}
	}},
}

// Folder returns the message for a folder name below app/ that belongs to another framework, as
// the error of the analysis gives it in place of its own.
func Folder(name string) (string, bool) {
	for _, f := range folderRules {
		if m := f.re.FindStringSubmatch(name); m != nil {
			return f.rule(m).Message(), true
		}
	}
	return "", false
}
