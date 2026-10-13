package analyze

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"
)

// checkTS reports every part of t that has no TypeScript form. A Load result is the JSON that
// encoding/json writes, so the rules follow encoding/json, restricted to a small set that maps
// to TypeScript directly. Code generation relies on exactly these rules:
//
//   - bool is boolean. Integer and float kinds (not uintptr) are number. string is string.
//   - time.Time is string (RFC 3339).
//   - []byte (any slice whose element kind is uint8) is string, base64 encoded.
//   - Other slices and arrays are T[].
//   - map[K]V with K of string kind is Record<string, V>.
//   - *T is T | null.
//   - A struct is an object with one property per exported field. The property name is the name
//     in the json tag, else the Go field name. Fields tagged json:"-" and unexported fields are
//     left out. A field tagged omitempty or omitzero is optional. Named types become named TS
//     types, so recursive types are allowed.
//
// Rejected: complex numbers, uintptr, unsafe.Pointer, chan, func, interfaces (including any and
// error), maps whose key is not a string kind, embedded fields without a json name, the json
// ",string" option, two fields with the same JSON name, and named types other than time.Time
// that define MarshalJSON or MarshalText, whose JSON form the analyzer cannot see.
func checkTS(t types.Type) []tsProblem {
	c := tsChecker{seen: map[string]bool{}}
	c.check(t, rootLabel(t))
	return c.problems
}

// tsProblem is one part of a type with no TypeScript form. path locates it from the root type,
// such as "Order.Items[].Meta". pos is the declaration of the offending field, if any.
type tsProblem struct {
	path string
	msg  string
	pos  *types.Var
}

type tsChecker struct {
	seen     map[string]bool // named types already checked, by type string
	problems []tsProblem
	field    *types.Var // field being checked, for the position of a problem
}

func (c *tsChecker) fail(path, format string, args ...any) {
	c.problems = append(c.problems, tsProblem{path: path, msg: fmt.Sprintf(format, args...), pos: c.field})
}

func rootLabel(t types.Type) string {
	for {
		switch u := types.Unalias(t).(type) {
		case *types.Pointer:
			t = u.Elem()
			continue
		case *types.Named:
			return u.Obj().Name()
		}
		return types.TypeString(t, shortQualifier)
	}
}

func shortQualifier(p *types.Package) string { return p.Name() }

func (c *tsChecker) check(t types.Type, path string) {
	typeName := types.TypeString(t, shortQualifier) // before Unalias, so any stays any
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == "time" && obj.Name() == "Time" {
			return
		}
		key := types.TypeString(named, nil)
		if c.seen[key] {
			return
		}
		c.seen[key] = true
		for _, m := range []string{"MarshalJSON", "MarshalText"} {
			if obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), true, nil, m); obj != nil {
				if _, isFunc := obj.(*types.Func); isFunc {
					c.fail(path, "%s defines %s, so its JSON form is unknown to the analyzer", typeName, m)
					return
				}
			}
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		info := u.Info()
		switch {
		case info&types.IsBoolean != 0, info&types.IsString != 0:
		case info&(types.IsInteger|types.IsFloat) != 0 && u.Kind() != types.Uintptr:
		default:
			c.fail(path, "%s has no TypeScript form", typeName)
		}
	case *types.Pointer:
		c.check(u.Elem(), path)
	case *types.Slice:
		if b, ok := types.Unalias(u.Elem()).Underlying().(*types.Basic); ok && b.Kind() == types.Uint8 {
			return
		}
		c.check(u.Elem(), path+"[]")
	case *types.Array:
		c.check(u.Elem(), path+"[]")
	case *types.Map:
		if b, ok := types.Unalias(u.Key()).Underlying().(*types.Basic); !ok || b.Info()&types.IsString == 0 {
			c.fail(path, "%s has no TypeScript form: map keys must be strings", typeName)
			return
		}
		c.check(u.Elem(), path+"[string]")
	case *types.Struct:
		c.checkStruct(u, path)
	case *types.Interface:
		c.fail(path, "%s has no TypeScript form: an interface has no fixed JSON shape", typeName)
	default: // chan, func, type parameters
		c.fail(path, "%s has no TypeScript form", typeName)
	}
}

func (c *tsChecker) checkStruct(s *types.Struct, path string) {
	names := map[string]bool{}
	outer := c.field
	defer func() { c.field = outer }()
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		c.field = f
		fieldPath := path + "." + f.Name()
		tag := reflect.StructTag(s.Tag(i)).Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Embedded() && name == "" {
			c.fail(fieldPath, "embedded field %s needs a json name or json:\"-\"", f.Name())
			continue
		}
		if !f.Exported() && !f.Embedded() {
			continue
		}
		if name == "" {
			name = f.Name()
		}
		for _, opt := range strings.Split(opts, ",") {
			if opt == "string" {
				c.fail(fieldPath, "the json \",string\" option is not supported")
			}
		}
		if names[name] {
			c.fail(fieldPath, "JSON name %q is used by two fields, encoding/json would drop both", name)
			continue
		}
		names[name] = true
		c.check(f.Type(), fieldPath)
	}
}
