package analyze

import (
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// named declares type name struct{ fields } in pkg. Fields are given as name, json tag and type,
// and a nil type refers to the type being declared.
func named(pkg *types.Package, name string, fields ...any) *types.Named {
	n := types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), nil, nil)
	var vars []*types.Var
	var tags []string
	for i := 0; i < len(fields); i += 3 {
		t, _ := fields[i+2].(types.Type)
		if t == nil {
			t = n
		}
		vars = append(vars, types.NewField(token.NoPos, pkg, fields[i].(string), t, false))
		tags = append(tags, fields[i+1].(string))
	}
	n.SetUnderlying(types.NewStruct(vars, tags))
	return n
}

// shapes runs TSShapes for one root type with the given own package and export name.
func shapesOf(t *testing.T, root types.Type, own, export string) (string, []string) {
	t.Helper()
	shapes, decls, _, err := TSShapes([]types.Type{root}, TSScope{Own: own, Exports: []string{export}})
	if err != nil {
		t.Fatalf("TSShapes: %v", err)
	}
	return shapes[0], decls
}

func sameLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n--\n") != strings.Join(want, "\n--\n") {
		t.Errorf("%s\n got %q\nwant %q", what, got, want)
	}
}

// A named struct is declared once under its Go name, with a line per field, and every use of it
// is the name, however deep it sits.
func TestTSShapeDeclaresNamedStructs(t *testing.T) {
	pkg := types.NewPackage("example.com/app/orders", "orders")
	item := named(pkg, "Item", "SKU", `json:"sku"`, types.Typ[types.String])
	order := named(pkg, "Order",
		"Items", `json:"items"`, types.NewSlice(item),
		"First", `json:"first"`, types.NewPointer(item),
		"Extra", `json:"extra,omitempty"`, item,
		"Note", `json:"note"`, types.NewStruct([]*types.Var{
			types.NewField(token.NoPos, pkg, "Text", types.Typ[types.String], false),
		}, []string{`json:"text"`}))
	ts, decls := shapesOf(t, types.NewSlice(order), pkg.Path(), "PageData")
	if ts != "Order[]" {
		t.Errorf("shape %s", ts)
	}
	sameLines(t, "decls", decls, []string{
		"export type Order = {\n  items: Item[]\n  first: Item | null\n  extra?: Item\n  note: { text: string }\n}",
		"export type Item = {\n  sku: string\n}",
	})
}

func TestTSShapeKeepsNonStructsAndSpecialTypesInline(t *testing.T) {
	pkg := types.NewPackage("example.com/app/p", "p")
	status := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Status", nil), types.Typ[types.String], nil)
	list := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Tags", nil), types.NewSlice(types.Typ[types.String]), nil)
	set := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Counts", nil), types.NewMap(types.Typ[types.String], types.Typ[types.Int]), nil)
	data := named(pkg, "Data", "Status", `json:"status"`, status, "Tags", `json:"tags"`, list, "Counts", `json:"counts"`, set)
	ts, decls := shapesOf(t, data, pkg.Path(), "PageData")
	if ts != "Data" {
		t.Errorf("shape %s", ts)
	}
	sameLines(t, "decls", decls, []string{
		"export type Data = {\n  status: string\n  tags: string[]\n  counts: Record<string, number>\n}",
	})
}

// A generic instance and a type declared inside a function are written inline, and the structs
// they hold are declared.
func TestTSShapeKeepsGenericInstancesAndLocalTypesInline(t *testing.T) {
	pkg := types.NewPackage("example.com/app/p", "p")
	item := named(pkg, "Item", "ID", `json:"id"`, types.Typ[types.Int])
	// type Page[T any] struct{ Rows []T }
	tp := types.NewTypeParam(types.NewTypeName(token.NoPos, pkg, "T", nil), types.NewInterfaceType(nil, nil))
	page := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Page", nil), nil, nil)
	page.SetTypeParams([]*types.TypeParam{tp})
	page.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, pkg, "Rows", types.NewSlice(tp), false),
	}, []string{`json:"rows"`}))
	inst, err := types.Instantiate(nil, page, []types.Type{item}, true)
	if err != nil {
		t.Fatal(err)
	}
	fn := types.NewScope(pkg.Scope(), token.NoPos, token.NoPos, "func")
	local := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Local", nil), types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, pkg, "Item", item, false),
	}, []string{`json:"item"`}), nil)
	fn.Insert(local.Obj())
	data := named(pkg, "Data", "Page", `json:"page"`, inst, "Local", `json:"local"`, local)
	ts, decls := shapesOf(t, data, pkg.Path(), "PageData")
	if ts != "Data" {
		t.Errorf("shape %s", ts)
	}
	sameLines(t, "decls", decls, []string{
		"export type Data = {\n  page: { rows: Item[] }\n  local: { item: Item }\n}",
		"export type Item = {\n  id: number\n}",
	})
}

// The type of this folder's package keeps its name, a type of another package gets the name of
// its package in front, and when that still collides the struct is written inline.
func TestTSShapeResolvesEqualNames(t *testing.T) {
	own := types.NewPackage("example.com/app/shop", "shop")
	cart := types.NewPackage("example.com/app/cart", "cart")
	cart2 := types.NewPackage("example.com/other/cart", "cart")
	ownOrder := named(own, "Order", "ID", `json:"id"`, types.Typ[types.Int])
	cartOrder := named(cart, "Order", "Lines", `json:"lines"`, types.Typ[types.Int])
	cartLine := named(cart, "Line", "N", `json:"n"`, types.Typ[types.Int])
	cart2Line := named(cart2, "Line", "M", `json:"m"`, types.Typ[types.Int])
	data := named(own, "Data", "A", `json:"a"`, ownOrder, "B", `json:"b"`, cartOrder, "C", `json:"c"`, cartLine, "D", `json:"d"`, cart2Line)
	ts, decls := shapesOf(t, data, own.Path(), "PageData")
	if ts != "Data" {
		t.Errorf("shape %s", ts)
	}
	sameLines(t, "decls", decls, []string{
		"export type Data = {\n  a: Order\n  b: CartOrder\n  c: { n: number }\n  d: { m: number }\n}",
		"export type Order = {\n  id: number\n}",
		"export type CartOrder = {\n  lines: number\n}",
	})
}

// The names do not depend on the order the types are met in.
func TestTSShapeNamesDoNotDependOnOrder(t *testing.T) {
	own := types.NewPackage("example.com/app/shop", "shop")
	a := types.NewPackage("example.com/a/cart", "cart")
	b := types.NewPackage("example.com/b/cart", "cart")
	oa := named(a, "Order", "X", `json:"x"`, types.Typ[types.Int])
	ob := named(b, "Order", "Y", `json:"y"`, types.Typ[types.Int])
	d1 := named(own, "Data", "A", `json:"a"`, oa, "B", `json:"b"`, ob)
	d2 := named(own, "Data", "B", `json:"b"`, ob, "A", `json:"a"`, oa)
	_, decls1 := shapesOf(t, d1, own.Path(), "PageData")
	_, decls2 := shapesOf(t, d2, own.Path(), "PageData")
	if strings.Contains(strings.Join(decls1, ""), "type CartOrder") || strings.Contains(strings.Join(decls2, ""), "type CartOrder") {
		t.Errorf("two types that still collide are declared: %q %q", decls1, decls2)
	}
	if !strings.Contains(decls1[0], "a: { x: number }") || !strings.Contains(decls2[0], "a: { x: number }") {
		t.Errorf("inline forms differ: %q %q", decls1, decls2)
	}
}

// A struct named like something the module exports or uses gets the prefix Go, except the one
// that is the very type of that export.
func TestTSShapeReservedNames(t *testing.T) {
	pkg := types.NewPackage("example.com/app/p", "p")
	for _, name := range []string{"Params", "Record", "Blob", "HTML", "Promise", "Response", "CallOptions", "SessionData", "GETInput", "string", "delete"} {
		inner := named(pkg, name, "X", `json:"x"`, types.Typ[types.Int])
		data := named(pkg, "Data", "In", `json:"in"`, inner)
		_, decls := shapesOf(t, data, pkg.Path(), "PageData")
		want := "export type Go" + name + " = {\n  x: number\n}"
		if len(decls) != 2 || decls[1] != want {
			t.Errorf("%s: %q", name, decls)
		}
	}
	// The Load result of the page is the Go type PageData: one declaration and no alias.
	pd := named(pkg, "PageData", "X", `json:"x"`, types.Typ[types.Int])
	ts, decls := shapesOf(t, pd, pkg.Path(), "PageData")
	if ts != "PageData" {
		t.Errorf("shape %s", ts)
	}
	sameLines(t, "decls", decls, []string{"export type PageData = {\n  x: number\n}"})
	// Reached through a pointer it is no alias of the export, so it takes the prefix.
	ts, decls = shapesOf(t, types.NewPointer(pd), pkg.Path(), "PageData")
	if ts != "GoPageData | null" {
		t.Errorf("pointer shape %s", ts)
	}
	sameLines(t, "pointer decls", decls, []string{"export type GoPageData = {\n  x: number\n}"})
}

func TestTSShapeDeclaresRecursiveTypes(t *testing.T) {
	pkg := types.NewPackage("example.com/tree", "tree")
	node := named(pkg, "Node", "Name", `json:"name"`, types.Typ[types.String])
	node.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, pkg, "Name", types.Typ[types.String], false),
		types.NewField(token.NoPos, pkg, "Children", types.NewSlice(node), false),
	}, []string{`json:"name"`, `json:"children"`}))
	other := types.NewPackage("example.com/other", "other")
	otherNode := named(other, "Node", "Next", `json:"next"`, nil)
	otherNode.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, other, "Next", types.NewPointer(otherNode), false),
	}, []string{`json:"next"`}))
	data := named(pkg, "Data",
		"Root", `json:"root"`, node,
		"Again", `json:"again"`, node,
		"Chain", `json:"chain"`, otherNode,
		"Count", `json:"count"`, types.Typ[types.Int])

	ts, decls := shapesOf(t, data, pkg.Path(), "PageData")
	if ts != "Data" {
		t.Errorf("shape %s", ts)
	}
	sameLines(t, "decls", decls, []string{
		"export type Data = {\n  root: Node\n  again: Node\n  chain: OtherNode\n  count: number\n}",
		"export type Node = {\n  name: string\n  children: Node[]\n}",
		"export type OtherNode = {\n  next: OtherNode | null\n}",
	})
}

// A recursive struct that cannot take its name is still declared, under the Go prefix and a number.
func TestTSShapeRecursiveStructWithCollidingNameKeepsDeclaration(t *testing.T) {
	a := types.NewPackage("example.com/a/tree", "tree")
	b := types.NewPackage("example.com/b/tree", "tree")
	mk := func(p *types.Package) *types.Named {
		n := named(p, "Node", "X", `json:"x"`, types.Typ[types.Int])
		n.SetUnderlying(types.NewStruct([]*types.Var{
			types.NewField(token.NoPos, p, "Next", types.NewPointer(n), false),
		}, []string{`json:"next"`}))
		return n
	}
	data := named(types.NewPackage("example.com/app", "app"), "Data", "A", `json:"a"`, mk(a), "B", `json:"b"`, mk(b))
	_, decls := shapesOf(t, data, "example.com/app", "PageData")
	got := strings.Join(decls, "\n")
	if !strings.Contains(got, "export type GoNode = { next: GoNode | null }") || !strings.Contains(got, "export type GoNode2 = { next: GoNode2 | null }") {
		t.Errorf("decls %q", decls)
	}
}

func TestTSShapeRejectsTypesWithoutJSONForm(t *testing.T) {
	if _, _, _, err := TSShapes([]types.Type{types.NewChan(types.SendRecv, types.Typ[types.Int])}, TSScope{}); err == nil {
		t.Fatal("TSShapes accepted a channel")
	}
}
