package analyze

import (
	"encoding/json/v2"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"github.com/liustack/tifa/internal/content"
	"github.com/liustack/tifa/internal/content/scan"
	"golang.org/x/mod/module"
	"golang.org/x/tools/go/packages"
)

const contentPkg = "github.com/liustack/tifa/content"

// Content is what the app declares in content.go and what content/ holds.
type Content struct {
	// Collections are the collections of content.go, in the order of the file.
	Collections []Collection
	// File holds the entries of every collection as dist/content.json has them, drafts left out
	// unless Options.Drafts.
	File content.File
	// Warnings are problems that do not stop a build, such as entries without a translation.
	Warnings []string
	// Markdown lists the entries of File for the Vite plugin to turn into HTML.
	Markdown []content.Markdown

	// What the scan of content/ was given, for ScanContent: the collections that can be scanned,
	// the folder of every declared one, and the options.
	scan     []scan.Collection
	declared []string
	opts     scan.Options
}

// ScanContent reads content/ of the app at root again, for the collections of an earlier analysis
// c, as tifa dev does after a file under content/ changed, without the analysis of the Go code.
// It returns the new entries, or the problems as Errors, the same lines as the analysis gives.
// For an app without collections (c nil) it only checks that content/ holds no folder, and
// returns nil.
func ScanContent(root string, c *Content) (*Content, error) {
	if c == nil {
		if errs := scanErrors(scan.Scan(root, nil, nil, scan.Options{}).Errors); len(errs) > 0 {
			return nil, errs
		}
		return nil, nil
	}
	res := scan.Scan(root, c.scan, c.declared, c.opts)
	n := &Content{Collections: c.Collections, scan: c.scan, declared: c.declared, opts: c.opts}
	n.File, n.Warnings, n.Markdown = res.File, res.Warnings, res.Markdown
	if errs := append(scanErrors(res.Errors), bodyFileErrors(n.File)...); len(errs) > 0 {
		return nil, errs
	}
	return n, nil
}

// Fingerprint changes whenever the declarations of the collections change: their folders,
// languages, Match or the fields of their schema types.
func (c *Content) Fingerprint() string {
	if c == nil {
		return ""
	}
	data, err := json.Marshal(struct {
		Scan     []scan.Collection
		Declared []string
	}{c.scan, c.declared}, json.Deterministic(true))
	if err != nil {
		panic(err) // the fields are plain data
	}
	return string(data)
}

// Collection is one var X = content.Define[T](content.Source{...}) of content.go.
type Collection struct {
	Var       string
	Dir       string
	Languages []string
	Match     string
	// Type is T, qualified by its full import path.
	Type string
}

var folderName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func isDefine(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == contentPkg && fn.Name() == "Define"
}

// defineIdent returns the identifier Define of a call of content.Define[T], nil for another call.
func defineIdent(info *types.Info, call *ast.CallExpr) *ast.Ident {
	fun := ast.Unparen(call.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	var id *ast.Ident
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	}
	if id == nil || !isDefine(info.Uses[id]) {
		return nil
	}
	return id
}

// declaration is one call of content.Define written where it belongs.
type declaration struct {
	pkg  *packages.Package
	name *ast.Ident
	call *ast.CallExpr
}

// content checks the content collections: content.Define is called only as the value of a
// package-level var of content.go in the root package, with a content.Source literal and a schema
// type of the module. Then it scans content/ for the collections that passed. It returns nil for
// an app that declares no collection.
func (a *analyzer) content() *Content {
	var decls []declaration
	for _, pkg := range a.modulePackages {
		for _, f := range pkg.Syntax {
			name := a.fset.Position(f.Pos()).Filename
			allowed := map[*ast.Ident]declaration{}
			if filepath.Dir(name) == a.root && filepath.Base(name) == "content.go" {
				for _, decl := range f.Decls {
					gen, ok := decl.(*ast.GenDecl)
					if !ok || gen.Tok != token.VAR {
						continue
					}
					for _, spec := range gen.Specs {
						vs := spec.(*ast.ValueSpec)
						for i, v := range vs.Values {
							call, ok := ast.Unparen(v).(*ast.CallExpr)
							if !ok || len(vs.Names) != len(vs.Values) {
								continue
							}
							if id := defineIdent(pkg.TypesInfo, call); id != nil {
								allowed[id] = declaration{pkg: pkg, name: vs.Names[i], call: call}
							}
						}
					}
				}
			}
			seen := map[*ast.Ident]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				var id *ast.Ident
				var pos token.Pos
				switch x := n.(type) {
				case *ast.SelectorExpr:
					id, pos = x.Sel, x.Pos()
				case *ast.Ident:
					id, pos = x, x.Pos()
				default:
					return true
				}
				if seen[id] || !isDefine(pkg.TypesInfo.Uses[id]) {
					return true
				}
				seen[id] = true
				if d, ok := allowed[id]; ok {
					decls = append(decls, d)
					return true
				}
				a.errorAt(pos, "content.Define declares a collection, which is written only as the value of a package-level var in content.go in the root of the app, such as var Blog = content.Define[Post](content.Source{Dir: \"blog\"})")
				return true
			})
		}
	}
	if len(decls) == 0 {
		// content/ belongs to tifa even without content.go: each folder in it needs a
		// collection. Without content/ this is one failed ReadDir.
		a.errs = append(a.errs, scanErrors(scan.Scan(a.root, nil, nil, scan.Options{}).Errors)...)
		return nil
	}
	slices.SortFunc(decls, func(x, y declaration) int { return int(x.name.Pos() - y.name.Pos()) })
	c := &Content{File: content.File{}}
	var colls []scan.Collection
	var declared []string
	byDir := map[string]*ast.Ident{}
	for _, d := range decls {
		coll, schema, languagesAt, ok := a.declaration(d, byDir)
		if coll.Dir != "" {
			declared = append(declared, coll.Dir)
		}
		if !ok {
			continue
		}
		c.Collections = append(c.Collections, coll)
		colls = append(colls, scan.Collection{
			Name: coll.Dir, Languages: coll.Languages, Match: coll.Match, Schema: schema,
			Type: shortType(d.pkg.TypesInfo.Types[typeArg(d.call)].Type), LanguagesAt: languagesAt,
		})
	}
	c.scan, c.declared, c.opts = colls, declared, scan.Options{Drafts: a.opts.Drafts}
	res := scan.Scan(a.root, colls, declared, c.opts)
	a.errs = append(a.errs, scanErrors(res.Errors)...)
	c.File, c.Warnings, c.Markdown = res.File, res.Warnings, res.Markdown
	a.errs = append(a.errs, bodyFileErrors(c.File)...)
	return c
}

// bodyFileErrors reports the entries whose body would go into a file of dist that go:embed does
// not take, which allows the names of module.CheckFilePath only, and those whose body would go
// below the body of another entry, as into a folder: a.html/b.md next to a.md.
func bodyFileErrors(f content.File) []Error {
	sources := map[string]string{} // file of a body to the Markdown it comes from
	for _, recs := range f {
		for _, r := range recs {
			sources[r.File] = r.Source
		}
	}
	var errs []Error
	for _, name := range slices.Sorted(maps.Keys(f)) {
		for _, r := range f[name] {
			if err := module.CheckFilePath(r.File); err != nil {
				errs = append(errs, Error{Pos: token.Position{Filename: r.Source}, Msg: fmt.Sprintf("the body of this entry goes into %s, a name go:embed refuses: %v. Rename the file", r.File, err)})
			}
			for dir := path.Dir(r.File); dir != "." && dir != "/"; dir = path.Dir(dir) {
				if other, ok := sources[dir]; ok {
					errs = append(errs, Error{Pos: token.Position{Filename: r.Source}, Msg: fmt.Sprintf("the body of this entry goes into %s, below %s, which is the body of %s: a file cannot also be a folder. Rename one of the two", r.File, dir, other)})
					break
				}
			}
		}
	}
	return errs
}

// scanErrors reports the errors of a scan of content/.
func scanErrors(errs []scan.Error) Errors {
	var out Errors
	for _, e := range errs {
		out = append(out, Error{Pos: token.Position{Filename: e.File, Line: e.Line, Column: e.Col}, Msg: e.Msg})
	}
	return out
}

func typeArg(call *ast.CallExpr) ast.Expr {
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.IndexExpr:
		return f.Index
	case *ast.IndexListExpr:
		return f.Indices[0]
	}
	return nil
}

// declaration checks one declaration and reads its Source. Dir is set whenever it is a valid
// folder name, so that the folder counts as declared. ok is true when the collection can be
// scanned. byDir records the var of each folder, for folders read twice.
func (a *analyzer) declaration(d declaration, byDir map[string]*ast.Ident) (coll Collection, schema *scan.Node, languagesAt string, ok bool) {
	info := d.pkg.TypesInfo
	coll.Var = d.name.Name
	ok = true
	index := typeArg(d.call)
	if index == nil || len(d.call.Args) != 1 {
		return coll, nil, "", false // the type checker reports it
	}
	t := info.Types[index].Type
	coll.Type = types.TypeString(t, nil)
	if !a.schemaType(t) {
		a.errorAt(index.Pos(), "%s must be a struct type of this module that embeds content.Entry, not a pointer to it", shortType(t))
		ok = false
	} else {
		var problems []scan.Problem
		schema, problems = scan.FromTypes(t)
		for _, p := range problems {
			a.errorAt(p.Pos, "%s", p.Msg)
			ok = false
		}
	}
	arg := d.call.Args[0]
	lit, isLit := ast.Unparen(arg).(*ast.CompositeLit)
	if !isLit || lit.Type == nil || !isNamed(info.Types[lit].Type, contentPkg, "Source") {
		a.errorAt(arg.Pos(), "the argument of content.Define must be a content.Source literal written in place, so that tifa build can read it")
		return coll, nil, "", false
	}
	hasDir := false
	for _, el := range lit.Elts {
		kv, isKV := el.(*ast.KeyValueExpr)
		if !isKV {
			a.errorAt(el.Pos(), "write the fields of content.Source by name, such as content.Source{Dir: \"blog\"}")
			return coll, nil, "", false
		}
		key, _ := kv.Key.(*ast.Ident)
		if key == nil {
			continue // the type checker reports it
		}
		switch key.Name {
		case "Dir":
			hasDir = true
			dir, isStr := stringLit(kv.Value)
			switch {
			case !isStr:
				a.errorAt(kv.Value.Pos(), "Dir must be a string literal, such as \"blog\"")
				ok = false
			case !folderName.MatchString(dir):
				a.errorAt(kv.Value.Pos(), "Dir %q must be the name of one folder under content/: letters, digits, - and _", dir)
				ok = false
			default:
				coll.Dir = dir
				if info, err := os.Stat(filepath.Join(a.root, scan.Dir, dir)); err != nil || !info.IsDir() {
					a.errorAt(kv.Value.Pos(), "content/%s does not exist: create it, or fix Dir", dir)
					ok = false
				}
				if first, dup := byDir[dir]; dup {
					p := a.fset.Position(first.Pos())
					a.errorAt(d.name.Pos(), "%s reads content/%s, which %s reads too (%s:%d:%d)", d.name.Name, dir, first.Name, a.relFile(p.Filename), p.Line, p.Column)
					ok = false
				} else {
					byDir[dir] = d.name
				}
			}
		case "Languages":
			langs, good := a.languages(kv.Value, info)
			if !good {
				ok = false
				continue
			}
			coll.Languages = langs
			p := a.fset.Position(kv.Value.Pos())
			languagesAt = fmt.Sprintf("%s:%d:%d", a.relFile(p.Filename), p.Line, p.Column)
		case "Match":
			m, isStr := stringLit(kv.Value)
			if !isStr {
				a.errorAt(kv.Value.Pos(), "Match must be a string literal, such as \"*/*/index.md\"")
				ok = false
				continue
			}
			if err := scan.CheckMatch(m); err != nil {
				a.errorAt(kv.Value.Pos(), "%v", err)
				ok = false
				continue
			}
			coll.Match = m
		}
	}
	if !hasDir {
		a.errorAt(lit.Pos(), "content.Source needs Dir, the folder under content/ that holds the collection, such as Dir: \"blog\"")
		ok = false
	}
	return coll, schema, languagesAt, ok
}

// isLanguagesCall reports whether e is the call tifa.Languages().
func isLanguagesCall(info *types.Info, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	id := calleeIdent(call)
	if id == nil {
		return false
	}
	fn, ok := info.Uses[id].(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == tifaPath && fn.Name() == "Languages"
}

// languages reads tifa.Languages(), or a []string literal of string literals, each the name of a language folder.
func (a *analyzer) languages(e ast.Expr, info *types.Info) ([]string, bool) {
	if isLanguagesCall(info, e) {
		// The languages of the site, which the analysis worked out before it read the content.
		return slices.Clone(a.site), true
	}
	lit, ok := ast.Unparen(e).(*ast.CompositeLit)
	var langs []string
	if ok && lit.Type != nil {
		if s, isSlice := info.Types[lit].Type.(*types.Slice); isSlice && types.Identical(s.Elem(), types.Typ[types.String]) {
			for _, el := range lit.Elts {
				l, isStr := stringLit(el)
				if !isStr {
					ok = false
					break
				}
				langs = append(langs, l)
			}
		} else {
			ok = false
		}
	} else {
		ok = false
	}
	if !ok {
		a.errorAt(e.Pos(), "Languages must be tifa.Languages() or a []string literal of string literals, such as []string{\"zh-CN\", \"en\"}")
		return nil, false
	}
	for i, l := range langs {
		if !folderName.MatchString(l) {
			a.errorAt(e.Pos(), "language %q must be the name of one folder: letters, digits, - and _", l)
			return nil, false
		}
		if slices.Contains(langs[:i], l) {
			a.errorAt(e.Pos(), "Languages has %q twice", l)
			return nil, false
		}
	}
	return langs, true
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := ast.Unparen(e).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// schemaType reports whether t is a named struct type of the module that embeds content.Entry.
func (a *analyzer) schemaType(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || !a.modulePkgs[n.Obj().Pkg().Path()] {
		return false
	}
	s, ok := n.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := range s.NumFields() {
		if f := s.Field(i); f.Embedded() && isNamed(f.Type(), contentPkg, "Entry") {
			return true
		}
	}
	return false
}
