package foreign

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ScanTree reads the file names under app/ and the Go files named route.go, and returns the idioms
// of other frameworks in them. It reads files and never runs anything, so it works on an app that
// does not build. TypeScript is read by the driver, see CheckFacts.
func ScanTree(root string) ([]Finding, error) {
	appDir := filepath.Join(root, "app")
	if info, err := os.Stat(appDir); err != nil || !info.IsDir() {
		return nil, nil
	}
	dirs := map[string]map[string]bool{} // by directory relative to root, the names of its files
	var names []string                   // the files, relative to root
	err := filepath.WalkDir(appDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		dir := path.Dir(rel)
		if dirs[dir] == nil {
			dirs[dir] = map[string]bool{}
		}
		dirs[dir][d.Name()] = true
		names = append(names, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, rel := range names {
		base := path.Base(rel)
		if r, ok := fileRule(base, dirs[path.Dir(rel)], path.Dir(rel) == "app"); ok {
			out = append(out, finding(rel, 1, 1, r))
		}
		if base == "route.go" {
			out = append(out, goHandlers(root, rel)...)
		}
	}
	for _, name := range []string{"middleware.ts", "middleware.js", "middleware.mjs"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			out = append(out, finding(name, 1, 1, ruleMiddleware(name)))
		}
	}
	return sortFindings(out), nil
}

// fileRule returns the rule for a file name in a folder that holds the files siblings. A name that
// sits next to the Tifa file of the same name is a module of the app's own, and a Next.js name
// such as error.tsx only speaks in a folder that is a route.
func fileRule(base string, siblings map[string]bool, top bool) (Rule, bool) {
	route := siblings["page.tsx"] || siblings["layout.tsx"] || siblings["layout_.tsx"] || siblings["route.go"]
	switch {
	case svelteFile.MatchString(base):
		return ruleSvelteFile(base), true
	case remixIndex.MatchString(base):
		return ruleRemixIndex(base), true
	case remixFlat.MatchString(base):
		return ruleRemixFlat(base), true
	case nextPageExt.MatchString(base):
		kind := nextPageExt.FindStringSubmatch(base)[1]
		if siblings[kind+".tsx"] || kind == "layout" && siblings["layout_.tsx"] {
			return Rule{}, false
		}
		return ruleNextPage(base, kind), true
	case nextRouteTS.MatchString(base):
		if siblings["route.go"] {
			return Rule{}, false
		}
		return ruleNextRoute(base), true
	case nextSpecial.MatchString(base):
		if !route {
			return Rule{}, false
		}
		return ruleNextSpecial(base, nextSpecial.FindStringSubmatch(base)[1]), true
	case middleware.MatchString(base):
		return ruleMiddleware(base), top
	}
	return Rule{}, false
}

// handlerNames are the functions of route.go that take the Ctx.
var handlerNames = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "Load": true, "Layout": true}

// goFramework is a package whose request type another framework's handlers take.
type goFramework struct {
	path  string   // the import path, without a major version
	types []string // the names of its handler parameter types
	name  string   // how the message calls its handlers
}

var goFrameworks = []goFramework{
	{"net/http", []string{"ResponseWriter", "Request"}, "a net/http handler"},
	{"github.com/gin-gonic/gin", []string{"Context"}, "a gin handler"},
	{"github.com/labstack/echo", []string{"Context"}, "an Echo handler"},
	{"github.com/gofiber/fiber", []string{"Ctx"}, "a Fiber handler"},
}

var majorVersion = regexp.MustCompile(`^v\d+$`)

// goHandlers reads route.go and reports the top-level functions that Tifa calls and that take
// the request type of net/http, gin, Echo or Fiber. A method and a function of another name may
// take any type. The file is read as syntax: with the framework not in go.mod it does not type
// check, and a handler with a signature of another framework is wrong in any case.
func goHandlers(root, rel string) []Finding {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
	if err != nil {
		return nil // go list reports a syntax error
	}
	imports := map[string]string{} // local name to import path, major version left off
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		base := p
		if majorVersion.MatchString(path.Base(p)) {
			base = path.Dir(p)
		}
		name := path.Base(base)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports[name] = base
	}
	var out []Finding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !handlerNames[fn.Name.Name] || fn.Type.Params == nil {
			continue
		}
		if fw, ok := foreignParam(fn.Type.Params, imports); ok {
			pos := fset.Position(fn.Name.Pos())
			out = append(out, finding(rel, pos.Line, pos.Column, goRule(fn.Name.Name, fw)))
		}
	}
	return out
}

// foreignParam returns the framework whose request type one of the parameters has.
func foreignParam(params *ast.FieldList, imports map[string]string) (goFramework, bool) {
	for _, field := range params.List {
		var found goFramework
		ast.Inspect(field.Type, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			for _, fw := range goFrameworks {
				if imports[x.Name] == fw.path && contains(fw.types, sel.Sel.Name) {
					found = fw
				}
			}
			return found.path == ""
		})
		if found.path != "" {
			return found, true
		}
	}
	return goFramework{}, false
}

func goRule(name string, fw goFramework) Rule {
	instead := "a method handler takes the Ctx and returns its result: func POST(c *tifa.Ctx, in AddInput) (Todo, error) decodes the input and writes the result as JSON, and a returned tifa.Error{Status, Message} is the error answer. To write the response yourself, take only the Ctx (func POST(c *tifa.Ctx) error) and use c.Writer() and c.Request()"
	if name == "Load" || name == "Layout" {
		instead = name + " takes the Ctx and returns the data: func " + name + "(c *tifa.Ctx) (Data, error)"
	}
	return Rule{Says: name + " has the signature of " + fw.name, Instead: instead, Doc: docHandlers}
}

// SourceFiles lists the TypeScript and JavaScript files of the app that the check reads: those
// under app/ and hooks.ts, as absolute paths. Declaration files, tests and node_modules are left out.
func SourceFiles(root string) ([]string, error) {
	var files []string
	add := func(p string, d fs.DirEntry) {
		name := d.Name()
		switch filepath.Ext(name) {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs":
		default:
			return
		}
		if strings.HasSuffix(name, ".d.ts") || strings.Contains(name, ".test.") || strings.Contains(name, ".spec.") {
			return
		}
		files = append(files, p)
	}
	if hooks, err := os.Stat(filepath.Join(root, "hooks.ts")); err == nil && !hooks.IsDir() {
		files = append(files, filepath.Join(root, "hooks.ts"))
	}
	appDir := filepath.Join(root, "app")
	if info, err := os.Stat(appDir); err != nil || !info.IsDir() {
		return files, nil
	}
	err := filepath.WalkDir(appDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		add(p, d)
		return nil
	})
	sort.Strings(files)
	return files, err
}
