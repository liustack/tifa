package analyze

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/config"
)

// A guard in OnRequest asks which route matched, c.Pattern() == "/account". After a folder of app/
// is renamed the string names a route that is gone, the guard stops matching without a word, and
// the build passes. The build warns where the code compares c.Pattern() with a string constant
// that no route of the app can have, in the package of hooks.go, in these forms only:
//
//   - c.Pattern() == "x" and !=, on either side
//   - switch c.Pattern() { case "x": }
//   - strings.HasPrefix(c.Pattern(), "x"), where "x" must start at least one pattern
//
// c.Pattern() may be held in a local variable that is assigned once. A variable, a function result
// or anything else that is not a constant is not read, and nothing is followed into other
// functions or packages.

// guardPatterns is what c.Pattern() can be: the route paths as the manifest writes them (the form
// the server passes on), the built-in addresses that run OnRequest, and the Markdown version of
// each route. The empty pattern of an unmatched URL is always allowed.
type guardPatterns struct {
	routes   []string
	builtins []string
}

// builtinPatterns are the files the program answers itself (see the BUILT IN table of tifa routes)
// that run OnRequest with their own path as the pattern. /sitemap-<n>.xml is handled apart.
var builtinPatterns = []string{"/sitemap.xml", "/llms.txt", "/llms-full.txt", "/__tifa/session"}

var sitemapPartPattern = regexp.MustCompile(`^/sitemap-[1-9][0-9]*\.xml$`)

// sitemapPrefix is a start of /sitemap-<n>.xml.
var sitemapPrefix = regexp.MustCompile(`^/sitemap-?[0-9]*(\.xml)?$`)

func newGuardPatterns(routes []*route) guardPatterns {
	g := guardPatterns{builtins: builtinPatterns}
	for _, r := range routes {
		g.routes = append(g.routes, r.Path)
	}
	return g
}

// all lists every fixed pattern, the Markdown versions included.
func (g guardPatterns) all() []string {
	out := slices.Clone(g.routes)
	for _, p := range g.routes {
		if p == "/" {
			out = append(out, "/index.md")
		} else {
			out = append(out, p+".md")
		}
	}
	return append(out, g.builtins...)
}

func (g guardPatterns) matches(lit string, prefix bool) bool {
	if lit == "" {
		return true // the pattern of an unmatched URL, or a prefix of everything
	}
	if prefix {
		if sitemapPrefix.MatchString(lit) {
			return true
		}
		return slices.ContainsFunc(g.all(), func(p string) bool { return strings.HasPrefix(p, lit) })
	}
	return slices.Contains(g.all(), lit) || sitemapPartPattern.MatchString(lit)
}

// nearest returns up to three route patterns closest to lit, as a prefix of the pattern when the
// guard compares a prefix.
func (g guardPatterns) nearest(lit string, prefix bool) []string {
	type cand struct {
		p    string
		dist int
	}
	var cs []cand
	for _, p := range g.routes {
		q := p
		if prefix && len(q) > len(lit) {
			q = q[:len(lit)]
		}
		cs = append(cs, cand{p, config.EditDistance(lit, q)})
	}
	slices.SortStableFunc(cs, func(a, b cand) int { return a.dist - b.dist })
	var out []string
	for _, c := range cs[:min(3, len(cs))] {
		if c.dist*2 <= len(lit) {
			out = append(out, c.p) // a far one would only mislead
		}
	}
	return out
}

func (a *analyzer) guardWarnings(routes []*route) []Warning {
	pk := a.rootPk
	if pk == nil || pk.TypesInfo == nil || len(a.broken) > 0 || len(routes) == 0 {
		return nil
	}
	for _, r := range routes {
		if !r.valid || r.broken {
			return nil // the list of routes is not whole, so no string can be called unmatched
		}
	}
	g := newGuardPatterns(routes)
	info := pk.TypesInfo
	held := heldPatterns(info, pk.Syntax)
	isPattern := func(e ast.Expr) bool {
		if id, ok := ast.Unparen(e).(*ast.Ident); ok {
			return held[info.Uses[id]]
		}
		return isPatternCall(info, e)
	}
	var out []Warning
	check := func(e ast.Expr, prefix bool) {
		tv := info.Types[e]
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			return
		}
		lit := constant.StringVal(tv.Value)
		if g.matches(lit, prefix) {
			return
		}
		out = append(out, a.guardWarning(e.Pos(), lit, prefix, g.nearest(lit, prefix)))
	}
	for _, file := range pk.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BinaryExpr:
				if n.Op != token.EQL && n.Op != token.NEQ {
					break
				}
				if isPattern(n.X) {
					check(n.Y, false)
				} else if isPattern(n.Y) {
					check(n.X, false)
				}
			case *ast.SwitchStmt:
				if n.Tag == nil || !isPattern(n.Tag) {
					break
				}
				for _, s := range n.Body.List {
					for _, e := range s.(*ast.CaseClause).List {
						check(e, false)
					}
				}
			case *ast.CallExpr:
				if len(n.Args) == 2 && isStringsHasPrefix(info, n) && isPattern(n.Args[0]) {
					check(n.Args[1], true)
				}
			}
			return true
		})
	}
	return out
}

func (a *analyzer) guardWarning(pos token.Pos, lit string, prefix bool, near []string) Warning {
	p := a.fset.Position(pos)
	p.Filename = a.relFile(p.Filename)
	what := "has that pattern"
	if prefix {
		what = "has a pattern that starts so"
	}
	msg := fmt.Sprintf("%q is compared with c.Pattern(), but no route of the app %s, so this guard never matches", lit, what)
	if len(near) > 0 {
		msg += ". Closest routes: " + strings.Join(near, ", ")
	}
	msg += ". c.Pattern() is the path of the matched route, such as /projects/{project}, without the method. Update the string to the route as it is now, or remove the guard"
	return Warning{Pos: p, Msg: msg + see(docGuard)}
}

// isPatternCall reports whether e is a call of the Pattern method of tifa.Ctx.
func isPatternCall(info *types.Info, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	s := info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal {
		return false
	}
	fn, ok := s.Obj().(*types.Func)
	if !ok || fn.Name() != "Pattern" {
		return false
	}
	named, ok := types.Unalias(derefType(fn.Signature().Recv().Type())).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == corePath && named.Obj().Name() == "Ctx"
}

func isStringsHasPrefix(info *types.Info, call *ast.CallExpr) bool {
	fn, ok := info.Uses[selectorName(call.Fun)].(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == "strings" && fn.Name() == "HasPrefix"
}

func selectorName(e ast.Expr) *ast.Ident {
	switch e := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.Ident:
		return e
	}
	return nil
}

// heldPatterns finds the local variables set once with v := c.Pattern() and never assigned again,
// so a guard may name the route in one place and compare it in several.
func heldPatterns(info *types.Info, files []*ast.File) map[types.Object]bool {
	held := map[types.Object]bool{}
	changed := map[types.Object]bool{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for i, l := range n.Lhs {
					id, ok := l.(*ast.Ident)
					if !ok {
						continue
					}
					if n.Tok == token.DEFINE && len(n.Lhs) == len(n.Rhs) && info.Defs[id] != nil {
						if isPatternCall(info, n.Rhs[i]) {
							held[info.Defs[id]] = true
						}
						continue
					}
					if obj := info.Uses[id]; obj != nil {
						changed[obj] = true
					}
				}
			case *ast.IncDecStmt:
				if id, ok := n.X.(*ast.Ident); ok {
					changed[info.Uses[id]] = true
				}
			case *ast.UnaryExpr:
				if id, ok := n.X.(*ast.Ident); ok && n.Op == token.AND {
					changed[info.Uses[id]] = true
				}
			}
			return true
		})
	}
	for obj := range changed {
		delete(held, obj)
	}
	return held
}
