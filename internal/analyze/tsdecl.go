package analyze

import (
	"errors"
	"go/types"
	"slices"
	"strconv"
)

// TSShape returns the TypeScript type of the JSON that json.Marshal writes for a Load result of
// type t, following the rules of checkTS. A named type that refers to itself cannot be written
// inline, so each one gets a declaration such as "type GoNode = { children: GoNode[] | null }"
// and is referred to by name. A declared name is the Go name with the prefix Go, so it never
// shadows a TypeScript keyword, a built-in type such as Record or a name of the generated
// module, and gains a number when two types share a name (GoNode, GoNode2). The result is the
// same for the same t. Types that checkTS rejects return an error.
func TSShape(t types.Type) (ts string, decls []string, err error) {
	shapes, decls, err := TSShapes([]types.Type{t})
	if err != nil {
		return "", nil, err
	}
	return shapes[0], decls, nil
}

// TSShapes is TSShape for several types written into one module: the declarations of their
// recursive types are shared, so a type that two of them hold is declared once.
func TSShapes(ts []types.Type) (shapes, decls []string, err error) {
	d := &tsDecls{names: map[tsKey]string{}, declared: map[tsKey]string{}, bodies: map[[2]string]string{}}
	var errs []error
	for _, t := range ts {
		c := tsChecker{active: map[tsKey]bool{}, decls: d}
		shapes = append(shapes, c.encode(t, rootLabel(t), false, false))
		for _, p := range c.problems {
			errs = append(errs, errors.New(p.path+": "+p.msg))
		}
	}
	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	return shapes, d.decls, nil
}

// tsDecls collects the declarations of recursive named types while a type is written.
type tsDecls struct {
	names    map[tsKey]string     // names given to types met again while being written
	declared map[tsKey]string     // types whose declaration is complete
	bodies   map[[2]string]string // declared name by Go type and TypeScript body
	taken    []string
	decls    []string
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
	d.decls = append(d.decls, "type "+name+" = "+ts)
	d.declared[key] = name
	d.bodies[[2]string{key.typ, ts}] = name
	return name
}
