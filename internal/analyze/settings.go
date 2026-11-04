package analyze

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"

	"github.com/liustack/tifa/internal/config"
)

// settingFuncs are the functions of package tifa that define a setting of the app.
var settingFuncs = map[string]bool{"String": true, "Strings": true, "Int": true, "Bool": true, "Duration": true, "Size": true, "Secret": true}

func isSettingFunc(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == tifaPath && fn.Signature().Recv() == nil && settingFuncs[fn.Name()]
}

// settings checks the settings the app defines and returns their keys in the order of config.go.
// tifa.String and the like may only be called as the value of a package-level var in config.go
// of the root package, which the generated main imports, so that every setting is defined before
// main runs. The key must be a string literal that is a valid key, and its flag and environment
// variable must be those of no other setting, of the app or of tifa.
func (a *analyzer) settings() []string {
	var keys []string
	for _, pkg := range a.modulePackages {
		for _, f := range pkg.Syntax {
			name := a.fset.Position(f.Pos()).Filename
			allowed := map[*ast.Ident]*ast.CallExpr{}
			if filepath.Dir(name) == a.root && filepath.Base(name) == "config.go" {
				for _, decl := range f.Decls {
					gen, ok := decl.(*ast.GenDecl)
					if !ok || gen.Tok != token.VAR {
						continue
					}
					for _, spec := range gen.Specs {
						for _, v := range spec.(*ast.ValueSpec).Values {
							if call, ok := ast.Unparen(v).(*ast.CallExpr); ok {
								if id := calleeIdent(call); id != nil && isSettingFunc(pkg.TypesInfo.Uses[id]) {
									allowed[id] = call
								}
							}
						}
					}
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || !isSettingFunc(pkg.TypesInfo.Uses[id]) {
					return true
				}
				call, ok := allowed[id]
				if !ok {
					a.errorAt(id.Pos(), "tifa.%s defines a setting of the app, which is written only in a package-level var of config.go in the root of the app, such as var DBURL = tifa.String(\"db.url\", \"app.db\", \"Database file\")", id.Name)
					return true
				}
				if key, ok := a.settingKey(id.Name, call, keys); ok {
					keys = append(keys, key)
				}
				return true
			})
		}
	}
	return keys
}

// settingKey reads the key of the call of tifa.<fn> and checks it against the keys before it.
func (a *analyzer) settingKey(fn string, call *ast.CallExpr, keys []string) (string, bool) {
	if len(call.Args) == 0 {
		return "", false // the type checker reports it
	}
	lit, ok := ast.Unparen(call.Args[0]).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		a.errorAt(call.Args[0].Pos(), "the key of tifa.%s must be a string literal, such as \"db.url\", so that tifa build can read it", fn)
		return "", false
	}
	key, err := strconv.Unquote(lit.Value)
	if err == nil {
		err = config.CheckKey(key)
	}
	if err == nil {
		err = config.Clash(key, keys)
	}
	if err != nil {
		a.errorAt(lit.Pos(), "%v", err)
		return "", false
	}
	return key, true
}
