package scan

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/liustack/tifa/internal/content/scan/internal/schemas"
	vetless "github.com/liustack/tifa/internal/content/scan/testdata/schemas"
)

// TestTreesOfReflectAndTypesAgree reads each type of package schemas with reflect and with
// go/types and checks that both give the same tree and the same problems.
func TestTreesOfReflectAndTypesAgree(t *testing.T) {
	compareTrees(t, "github.com/liustack/tifa/internal/content/scan/internal/schemas", schemas.All)
	compareTrees(t, "github.com/liustack/tifa/internal/content/scan/testdata/schemas", vetless.All)
}

// compareTrees compares the trees of the types all lists, which package path declares.
func compareTrees(t *testing.T, path string, all map[string]any) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedTypes | packages.NeedName}, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("loading %s: %v", path, pkgs)
	}
	scope := pkgs[0].Types.Scope()
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range all {
			if !yield(k) {
				return
			}
		}
	}) {
		t.Run(name, func(t *testing.T) {
			obj := scope.Lookup(name)
			if obj == nil {
				t.Fatalf("no type %s in %s", name, path)
			}
			fromTypes, typesProblems := FromTypes(obj.(*types.TypeName).Type())
			fromReflect, reflectProblems := FromReflect(reflect.TypeOf(all[name]))
			if a, b := dump(fromTypes), dump(fromReflect); a != b {
				t.Errorf("trees differ\ngo/types:\n%s\nreflect:\n%s", a, b)
			}
			if a, b := messages(typesProblems), messages(reflectProblems); !slices.Equal(a, b) {
				t.Errorf("problems differ\ngo/types: %q\nreflect:  %q", a, b)
			}
			for _, p := range typesProblems {
				if !p.Pos.IsValid() {
					t.Errorf("problem %q has no position", p.Msg)
				}
			}
			if want, ok := wantTrees[name]; ok && dump(fromReflect) != want {
				t.Errorf("tree of %s:\n%s\nwant:\n%s", name, dump(fromReflect), want)
			}
			if want, ok := wantProblems[name]; ok && !slices.Equal(messages(reflectProblems), want) {
				t.Errorf("problems of %s:\n%q\nwant:\n%q", name, messages(reflectProblems), want)
			}
		})
	}
}

var wantTrees = map[string]string{
	"Basic": `struct
  s! string
  b! bool
  i! int64
  i8! int8
  i16! int16
  i32! int32
  i64! int64
  u! uint64
  u8! uint8
  u16! uint16
  u32! uint32
  u64! uint64
  f32! float32
  f64! float64
  T! time
`,
	"Nested": `struct
  image! struct
    src! string
    width int64
  cover pointer struct (seen)
  gallery slice struct (seen)
  byName map struct (seen)
  lists slice slice string
  inline! struct
    a! string
`,
	"Embedding": `struct
  author! string
  note string
  meta! struct
    author! string
    note string
  Title! string
`,
	"Recursive": `struct
  root! struct
    name! string
    children slice struct (seen)
    parent pointer struct (seen)
`,
	"RecursiveNamed": `struct
  nodes slice slice (seen)
  tree map map (seen)
  ring slice pointer slice (seen)
  again slice (seen)
`,
	"OddNames": `struct
  a b! string
  x-y.z! string
`,
	"Optional": `struct
  a string
  b int64
  c pointer int64
`,
}

var wantProblems = map[string][]string{
	"BadTags": {
		"schemas.BadTags.C has the json option \"case:ignore\": a schema field takes only omitempty and omitzero",
		"schemas.BadTags.Q has the json option \"string\": a schema field takes only omitempty and omitzero",
		"schemas.BadTags.Y has a yaml tag: front matter keys are the json names, remove the yaml tag",
	},
	"Reserved": {
		"schemas.Reserved.Date has the json name \"date\", which content.Entry uses for the time an entry is published",
		"schemas.Reserved.Key has the json name \"translationKey\", which tifa reads to link the translations of an entry",
		"schemas.Reserved.Words has the json name \"words\", which content.Entry uses for the number of words of the body",
	},
	"NoEntry":        nil,
	"RecursiveNamed": nil,
	"UnexportedTag": {
		"schemas.UnexportedTag.note is unexported and has a json tag, which json v2 refuses: export the field, remove the tag or tag it json:\"-\"",
	},
	"BadNames": {
		"schemas.BadNames.Quoted has the json name \"'title'\", which json v2 refuses: use letters, digits and the punctuation !#$%&()*+-./:;<=>?@[]^_{|}~ or space",
	},
	"OddNames": nil,
	"Optional": {
		"schemas.Optional.E has a json tag with an empty option, which json v2 refuses: remove the extra comma",
		"schemas.Optional.F has the json option \"omitempty\" twice",
	},
	"RootJSON": {"schemas.RootJSON reads its own JSON, so front matter cannot be checked against it: remove its UnmarshalJSON, UnmarshalJSONFrom or UnmarshalText method"},
	"RootText": {"schemas.RootText reads its own JSON, so front matter cannot be checked against it: remove its UnmarshalJSON, UnmarshalJSONFrom or UnmarshalText method"},
	"RootFrom": {"schemas.RootFrom reads its own JSON, so front matter cannot be checked against it: remove its UnmarshalJSON, UnmarshalJSONFrom or UnmarshalText method"},
	"TwiceEntry": {
		"schemas.TwiceEntry.E is a second embedded content.Entry: a schema embeds it once, as its own field",
	},
	"DeepEntry": {
		"schemas.DeepEntry.WithEntry.Entry is a second embedded content.Entry: a schema embeds it once, as its own field",
	},
	"SelfPointer": {
		"schemas.SelfPointer.Loop has the type schemas.Loop, a pointer that points to itself, which front matter cannot hold: " + holdable,
	},
	"BadEmbedding": {
		"schemas.BadEmbedding.Image is an embedded pointer: embed the struct itself, or give the field a name",
		"schemas.BadEmbedding.Status is embedded and is not a struct whose fields front matter holds: give it a json name",
		"schemas.BadEmbedding.hidden is an unexported embedded field: export its type, or give the field a name",
	},
	"Clash": {
		"schemas.Clash.Meta.Author and schemas.Clash.Author have the same json name \"author\": rename one or tag it json:\"-\"",
	},
}

// dump writes a tree as indented text, required fields marked with !.
func dump(n *Node) string {
	var b strings.Builder
	seen := map[*Node]bool{}
	var walk func(n *Node, indent string)
	walk = func(n *Node, indent string) {
		switch n.Kind {
		case String:
			b.WriteString("string\n")
		case Bool:
			b.WriteString("bool\n")
		case Int:
			fmt.Fprintf(&b, "int%d\n", n.Bits)
		case Uint:
			fmt.Fprintf(&b, "uint%d\n", n.Bits)
		case Float:
			fmt.Fprintf(&b, "float%d\n", n.Bits)
		case Time:
			b.WriteString("time\n")
		case Pointer, Slice, Map:
			name := []string{Pointer: "pointer", Slice: "slice", Map: "map"}[n.Kind]
			if seen[n] {
				b.WriteString(name + " (seen)\n")
				return
			}
			seen[n] = true
			b.WriteString(name + " ")
			walk(n.Elem, indent)
		case Struct:
			if seen[n] {
				b.WriteString("struct (seen)\n")
				return
			}
			seen[n] = true
			b.WriteString("struct\n")
			for _, f := range n.Fields {
				mark := ""
				if f.Required {
					mark = "!"
				}
				fmt.Fprintf(&b, "%s  %s%s ", indent, f.Key, mark)
				walk(f.Node, indent+"  ")
			}
		default:
			fmt.Fprintf(&b, "kind %d\n", n.Kind)
		}
	}
	if n == nil {
		return "nil\n"
	}
	walk(n, "")
	return b.String()
}

func messages(ps []Problem) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Msg)
	}
	slices.Sort(out)
	return out
}
