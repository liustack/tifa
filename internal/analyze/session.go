package analyze

import (
	"cmp"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/packages"
)

// Session describes the session of the application: the one type S that tifa.SetSession and
// tifa.Session are called with. It is nil in the manifest when nothing calls tifa.SetSession.
type Session struct {
	// Type is the Go type S, qualified by full import paths.
	Type string `json:"type"`
	// Browser reports whether S or *S has the method Browser() M, and BrowserType is M.
	Browser     bool   `json:"browser"`
	BrowserType string `json:"browserType,omitempty"`
	// BrowserResult is M for code generation inside the same process. It satisfies the rules of checkTS.
	BrowserResult types.Type `json:"-"`
}

// sessionCall is one use of tifa.SetSession, tifa.Session or tifa.ClearSession.
type sessionCall struct {
	name string // "SetSession", "Session" or "ClearSession"
	pos  token.Pos
	typ  types.Type // the type argument S, nil for ClearSession
}

// sessionCalls lists the uses of the three session functions in the packages of the module that
// type check, sorted by position. Packages outside the module are not read: a call there with
// another type fails at run time.
func (a *analyzer) sessionCalls(pkgs []*packages.Package) []sessionCall {
	var calls []sessionCall
	for _, p := range pkgs {
		for id, obj := range p.TypesInfo.Uses {
			fn, ok := obj.(*types.Func)
			if !ok || fn.Pkg() == nil || fn.Pkg().Path() != tifaPath {
				continue
			}
			switch fn.Name() {
			case "SetSession", "Session", "ClearSession":
			default:
				continue
			}
			c := sessionCall{name: fn.Name(), pos: id.Pos()}
			if inst, ok := p.TypesInfo.Instances[id]; ok && inst.TypeArgs.Len() == 1 {
				c.typ = inst.TypeArgs.At(0)
			}
			calls = append(calls, c)
		}
	}
	slices.SortFunc(calls, func(x, y sessionCall) int { return cmp.Compare(x.pos, y.pos) })
	return calls
}

// session finds the session type of the application and checks it. pkgs are the packages of
// the module that type check.
func (a *analyzer) session(pkgs []*packages.Package) *Session {
	calls := a.sessionCalls(pkgs)
	var first *sessionCall // the first SetSession, which names the session type
	for i := range calls {
		if calls[i].name == "SetSession" {
			first = &calls[i]
			break
		}
	}
	if first == nil {
		for _, c := range calls {
			a.errorAt(c.pos, "tifa.%s is called, but there is no call of tifa.SetSession: a request can only have a session after one signs in", c.name)
			break // one report is enough, the fix is the same for every call
		}
		return nil
	}
	// A type argument that is still a type parameter after instantiation is a call inside a generic
	// function, where the analyzer cannot tell which type the callers use.
	generic := func(c sessionCall) bool {
		return c.typ != nil && containsTypeParam(c.typ)
	}
	if generic(*first) {
		a.errorAt(first.pos, "tifa.SetSession is called with a type parameter inside a generic function, where the analyzer cannot tell which session type it gets: call it with the session type itself, in a function that is not generic")
		return nil
	}
	ok := true
	firstPos := a.fset.Position(first.pos)
	firstPos.Filename = a.relFile(firstPos.Filename)
	for _, c := range calls {
		switch {
		case c.typ == nil:
		case generic(c):
			a.errorAt(c.pos, "tifa.%s is called with a type parameter inside a generic function, where the analyzer cannot tell which session type it gets: call it with the session type itself, in a function that is not generic", c.name)
		case !types.Identical(c.typ, first.typ):
			a.errorAt(c.pos, "tifa.%s[%s] uses another session type than tifa.SetSession[%s] at %s:%d: use one session type for the whole app", c.name, shortType(c.typ), shortType(first.typ), firstPos.Filename, firstPos.Line)
		}
	}
	s := first.typ
	if _, isStruct := s.Underlying().(*types.Struct); !isStruct {
		a.errorAt(first.pos, "the session type %s must be a struct: the session is stored as the JSON of its fields", typeString(s))
		return nil
	}
	if problems := checkInputTS(s); len(problems) > 0 {
		a.reportTS(first.pos, "session type", problems)
		ok = false
	}
	out := &Session{Type: typeString(s)}
	if fn := browserMethod(s); fn != nil {
		sig := fn.Signature()
		if sig.Params().Len() != 0 || sig.Results().Len() != 1 || sig.Variadic() {
			a.errorAt(fn.Pos(), "Browser must have the signature func() M: it gives the part of the session the browser may see, as one value")
			return nil
		}
		m := sig.Results().At(0).Type()
		if problems := checkTS(m); len(problems) > 0 {
			a.reportTS(fn.Pos(), "Browser result", problems)
			ok = false
		}
		out.Browser, out.BrowserType, out.BrowserResult = true, typeString(m), m
	}
	if !ok {
		return nil
	}
	return out
}

// browserMethod returns the method Browser of s or *s, nil when there is none.
func browserMethod(s types.Type) *types.Func {
	obj, _, _ := types.LookupFieldOrMethod(s, true, nil, "Browser")
	fn, _ := obj.(*types.Func)
	return fn
}

// containsTypeParam reports whether t is, or is built from, a type parameter.
func containsTypeParam(t types.Type) bool {
	found := false
	var walk func(t types.Type)
	seen := map[types.Type]bool{}
	walk = func(t types.Type) {
		if t == nil || found || seen[t] {
			return
		}
		seen[t] = true
		switch t := types.Unalias(t).(type) {
		case *types.TypeParam:
			found = true
		case *types.Named:
			for ta := range t.TypeArgs().Types() {
				walk(ta)
			}
		case *types.Pointer:
			walk(t.Elem())
		case *types.Slice:
			walk(t.Elem())
		case *types.Array:
			walk(t.Elem())
		case *types.Map:
			walk(t.Key())
			walk(t.Elem())
		case *types.Chan:
			walk(t.Elem())
		case *types.Struct:
			// Every field counts, also those json leaves out: they still make two instances two types.
			for f := range t.Fields() {
				walk(f.Type())
			}
		case *types.Signature:
			walk(t.Params())
			walk(t.Results())
		case *types.Tuple:
			for v := range t.Variables() {
				walk(v.Type())
			}
		case *types.Interface:
			for m := range t.ExplicitMethods() {
				walk(m.Type())
			}
			for e := range t.EmbeddedTypes() {
				walk(e)
			}
		case *types.Union:
			for term := range t.Terms() {
				walk(term.Type())
			}
		}
	}
	walk(t)
	return found
}

// isSessionWrite reports whether fn is tifa.SetSession or tifa.ClearSession, which change the
// cookie of the response and so cannot run in Load, Layout or what they call.
func isSessionWrite(fn *types.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == tifaPath && fn.Signature().Recv() == nil &&
		(fn.Name() == "SetSession" || fn.Name() == "ClearSession")
}
