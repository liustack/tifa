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

	ts, decls, err := TSShape(data, []string{"Data", "Params", "route"})
	if err != nil {
		t.Fatalf("TSShape: %v", err)
	}
	want := "{ root: Node; again: Node; chain: Node2; count: number }"
	if ts != want {
		t.Errorf("shape\n got %s\nwant %s", ts, want)
	}
	wantDecls := []string{
		"type Node = { name: string; children: Node[] | null }",
		"type Node2 = { next: Node2 | null }",
	}
	if strings.Join(decls, "\n") != strings.Join(wantDecls, "\n") {
		t.Errorf("decls\n got %q\nwant %q", decls, wantDecls)
	}
}

// A declared name never takes one of the reserved names, which the generated module exports.
func TestTSShapeAvoidsReservedNames(t *testing.T) {
	pkg := types.NewPackage("example.com/p", "p")
	data := named(pkg, "Data", "Next", `json:"next"`, nil)
	data.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, pkg, "Next", types.NewPointer(data), false),
	}, []string{`json:"next"`}))
	ts, decls, err := TSShape(data, []string{"Data"})
	if err != nil {
		t.Fatalf("TSShape: %v", err)
	}
	if ts != "Data2" || len(decls) != 1 || decls[0] != "type Data2 = { next: Data2 | null }" {
		t.Errorf("got %s %q", ts, decls)
	}
}

func TestTSShapeRejectsTypesWithoutJSONForm(t *testing.T) {
	if _, _, err := TSShape(types.NewChan(types.SendRecv, types.Typ[types.Int]), nil); err == nil {
		t.Fatal("TSShape accepted a channel")
	}
}
