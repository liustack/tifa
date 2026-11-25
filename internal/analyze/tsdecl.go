package analyze

import (
	"errors"
	"go/types"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TSScope says where the types of a generated module belong.
type TSScope struct {
	// Own is the import path of the package of the folder the module is generated for. A struct
	// of that package keeps its Go name when another package has a struct of the same name.
	Own string
	// Exports names what the module exports for the type at the same index, such as "PageData".
	// A struct that is that type, and is named so in Go, is declared under that name and needs no
	// alias. The slice may be shorter than the types, and "" means no export.
	Exports []string
}

// TSShape returns the TypeScript type of the JSON that json.Marshal writes for a Load result of
// type t. It is TSShapes for one type, see there.
func TSShape(t types.Type, scope TSScope) (ts string, decls []string, html bool, err error) {
	shapes, decls, html, err := TSShapes([]types.Type{t}, scope)
	if err != nil {
		return "", nil, false, err
	}
	return shapes[0], decls, html, nil
}

// TSShapes returns the TypeScript type of the JSON that json.Marshal writes for each of ts, as the
// Load results and the inputs and results of the handlers of one module, following the rules of
// checkTS. Every named struct (not generic, not declared inside a function) that the types use
// is declared once in decls as "export type Name = {...}" and written as its name. Other named
// types are written inline, except a recursive one, which cannot be, and gets a declaration
// too, named Go and its Go name. The names are chosen from the set of structs alone, so they do
// not depend on the order the types are met in:
//
//   - A struct keeps its Go name.
//   - Names the module exports or uses itself (see tsReserved) take the prefix Go.
//   - When several structs have one name, the one of scope.Own keeps it and the others take the
//     name of their package in front (cart.Order is CartOrder).
//   - A struct whose name still collides is written inline.
//
// Types that checkTS rejects return an error. html reports whether the type refers to HTML, the
// TypeScript type of tifa.HTML, which the module must import from the runtime.
func TSShapes(ts []types.Type, scope TSScope) (shapes, decls []string, html bool, err error) {
	// The first walk finds the structs, the second writes them with the names they got.
	found := &tsDecls{collect: true, found: map[string]*types.Named{}, names: map[tsKey]string{}, declared: map[tsKey]string{}, bodies: map[[2]string]string{}}
	for _, t := range ts {
		c := tsChecker{active: map[tsKey]bool{}, decls: found, files: true}
		c.encode(t, rootLabel(t), false)
	}
	exempt := map[string]string{}
	for i, t := range ts {
		if i >= len(scope.Exports) || scope.Exports[i] == "" {
			continue
		}
		n, ok := types.Unalias(t).(*types.Named)
		if !ok {
			continue
		}
		key := types.TypeString(n, nil)
		if _, ok := found.found[key]; ok && n.Obj().Name() == scope.Exports[i] {
			exempt[key] = scope.Exports[i]
		}
	}
	assigned := structNames(found.found, scope.Own, exempt)

	d := &tsDecls{assigned: assigned, emitted: map[string]bool{}, names: map[tsKey]string{}, declared: map[tsKey]string{}, bodies: map[[2]string]string{}}
	for _, name := range assigned {
		d.taken = append(d.taken, name)
	}
	var errs []error
	for _, t := range ts {
		// The checks accepted a file only where an input holds one, so one met here is that.
		c := tsChecker{active: map[tsKey]bool{}, decls: d, files: true}
		shapes = append(shapes, c.encode(t, rootLabel(t), false))
		for _, p := range c.problems {
			errs = append(errs, errors.New(p.path+": "+p.msg))
		}
	}
	if len(errs) > 0 {
		return nil, nil, false, errors.Join(errs...)
	}
	return shapes, d.decls, d.html, nil
}

// structNames chooses the name of each struct, by its key, and leaves out the structs that must
// stay inline. exempt holds the structs that are the type of an export of their own name.
func structNames(found map[string]*types.Named, own string, exempt map[string]string) map[string]string {
	final := map[string]string{}
	groups := map[string][]string{}
	for key, n := range found {
		if name, ok := exempt[key]; ok {
			final[key] = name
			continue
		}
		name := n.Obj().Name()
		if tsReserved(name) {
			name = "Go" + name
		}
		groups[name] = append(groups[name], key)
	}
	for name, keys := range groups {
		if len(keys) == 1 {
			final[keys[0]] = name
			continue
		}
		for _, key := range keys {
			obj := found[key].Obj()
			if obj.Pkg() != nil && obj.Pkg().Path() == own {
				final[key] = name
				continue
			}
			prefixed := obj.Name()
			if obj.Pkg() != nil {
				prefixed = upperFirst(obj.Pkg().Name()) + prefixed
			}
			if tsReserved(prefixed) {
				prefixed = "Go" + prefixed
			}
			final[key] = prefixed
		}
	}
	count := map[string]int{}
	for _, name := range final {
		count[name]++
	}
	// A name taken twice goes to the type of this folder's package that is called so by its own
	// name (a prefixed name of another package may equal it), and the others are written inline.
	for name, n := range count {
		if n < 2 {
			continue
		}
		keep := ""
		for key, kn := range final {
			obj := found[key].Obj()
			if kn == name && obj.Name() == name && obj.Pkg() != nil && obj.Pkg().Path() == own {
				keep = key
			}
		}
		for key, kn := range final {
			if kn == name && key != keep {
				delete(final, key)
			}
		}
	}
	return final
}

func upperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

// tsReservedNames are the names a declaration must not take: what route.gen.ts and session.gen.ts
// export, the names they use from TypeScript and the runtime, and the words TypeScript does not
// let a type be called.
var tsReservedNames = []string{
	"PageData", "LayoutData", "PageSearch", "LayoutSearch", "Params", "SessionData", "SessionEnabled",
	"Record", "Blob", "HTML", "Promise", "Response", "CallOptions",
	// TypeScript's own type names and the reserved words of a module, which is strict.
	"any", "unknown", "never", "object", "symbol", "bigint", "string", "number", "boolean",
	"undefined", "void", "null", "true", "false", "this", "super", "new", "delete", "typeof",
	"instanceof", "in", "of", "is", "as", "keyof", "infer", "readonly", "asserts", "enum",
	"class", "const", "let", "var", "function", "export", "import", "default", "extends",
	"implements", "interface", "package", "private", "protected", "public", "static", "yield",
	"await", "async", "do", "else", "for", "if", "while", "with", "try", "catch", "finally",
	"throw", "return", "switch", "case", "break", "continue", "debugger", "eval", "arguments",
	"type", "namespace", "module", "declare", "abstract", "get", "set", "intrinsic", "unique",
}

// tsReserved reports whether a declaration may not be called name: besides tsReservedNames, the
// input and output of any method handler function.
func tsReserved(name string) bool {
	if slices.Contains(tsReservedNames, name) {
		return true
	}
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		if name == m+"Input" || name == m+"Output" {
			return true
		}
	}
	return false
}

// tsDecls collects the declarations of a module while its types are written.
type tsDecls struct {
	// collect is set for the first walk, which only finds the structs, in found by key.
	collect bool
	found   map[string]*types.Named
	// assigned is the name of each struct that is declared, by key, and emitted the ones written.
	assigned map[string]string
	emitted  map[string]bool

	// What follows declares the types that are no struct and refer to themselves, and the
	// recursive structs that could not take their name.
	names    map[tsKey]string     // names given to types met again while being written
	declared map[tsKey]string     // types whose declaration is complete
	bodies   map[[2]string]string // declared name by Go type and TypeScript body
	taken    []string
	decls    []string
	html     bool // a type refers to tifa.HTML
}

// declarable reports whether n is a struct that gets a declaration of its own: a type of a
// package, not generic.
func declarable(n *types.Named) bool {
	if _, ok := n.Underlying().(*types.Struct); !ok || n.TypeArgs().Len() > 0 || n.TypeParams().Len() > 0 {
		return false
	}
	obj := n.Obj()
	return obj.Pkg() != nil && (obj.Parent() == nil || obj.Parent() == obj.Pkg().Scope())
}

// name returns the declared name of the type with key, choosing it on first use.
func (d *tsDecls) name(key tsKey, goName string) string {
	if name, ok := d.names[key]; ok {
		return name
	}
	name := "Go" + goName
	for i := 2; slices.Contains(d.taken, name); i++ {
		name = "Go" + goName + strconv.Itoa(i)
	}
	d.taken = append(d.taken, name)
	d.names[key] = name
	return name
}

// declare finishes writing the type with key. A type that was referred to by name while being
// written becomes a declaration and is replaced by its name, others stay inline.
func (d *tsDecls) declare(key tsKey, ts string) string {
	name, ok := d.names[key]
	if !ok {
		// The same Go type reached another way, through a pointer or not, may already be
		// declared with this very body.
		if name, ok := d.bodies[[2]string{key.typ, ts}]; ok {
			return name
		}
		return ts
	}
	d.decls = append(d.decls, "export type "+name+" = "+ts)
	d.declared[key] = name
	d.bodies[[2]string{key.typ, ts}] = name
	return name
}

// namedStruct writes the struct n by its name and declares it, the first time. It reports false
// when n is written like any other type.
func (c *tsChecker) namedStruct(n *types.Named, path string) (string, bool) {
	d := c.decls
	if !declarable(n) {
		return "", false
	}
	key := types.TypeString(n, nil)
	if d.collect {
		if _, seen := d.found[key]; !seen {
			d.found[key] = n
			c.objectProps(n.Underlying().(*types.Struct), path)
		}
		return key, true
	}
	name, ok := d.assigned[key]
	if !ok {
		return "", false
	}
	if d.emitted[key] {
		return name, true
	}
	d.emitted[key] = true
	// The slot keeps the declarations in the order the structs are first met.
	slot := len(d.decls)
	d.decls = append(d.decls, "")
	props := c.objectProps(n.Underlying().(*types.Struct), path)
	body := "{}"
	if len(props) > 0 {
		body = "{\n  " + strings.Join(props, "\n  ") + "\n}"
	}
	d.decls[slot] = "export type " + name + " = " + body
	return name, true
}
