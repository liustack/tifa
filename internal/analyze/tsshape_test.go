package analyze

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The derived shape must describe what encoding/json really writes. testdata/jsonshape/encode
// marshals sample values of the fixture's Load result with json.Marshal, under the default
// implementation of this toolchain (json v2 with v1 options since Go 1.27) and under the
// classic v1 implementation (GOEXPERIMENT=nojsonv2). Every document must match the shape.
func TestLoadResultShapeMatchesEncodingJSON(t *testing.T) {
	m, err := analyzeApp(t, "jsonshape")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	ts, problems := tsType(m.Routes[0].Load.Result)
	if len(problems) > 0 {
		t.Fatalf("problems: %+v", problems)
	}
	shape, err := parseTS(ts)
	if err != nil {
		t.Fatalf("parse %q: %v", ts, err)
	}
	// Node is written by name where it recurs, so it resolves to the object inside tree.
	refs := map[string]*tsNode{"Node": shape.prop("tree").alts[0]}

	outputs := map[string]string{}
	for _, experiment := range []string{"", "nojsonv2"} {
		cmd := exec.Command("go", "run", "./encode")
		cmd.Dir = filepath.Join("testdata", "jsonshape")
		cmd.Env = append(os.Environ(), "GOEXPERIMENT="+experiment)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("GOEXPERIMENT=%s go run ./encode: %v\n%s", experiment, err, stderr.String())
		}
		outputs[experiment] = string(out)
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) < 3 {
			t.Fatalf("GOEXPERIMENT=%s: want 3 documents, got:\n%s", experiment, out)
		}
		for _, line := range lines {
			var v any
			if err := json.Unmarshal([]byte(line), &v); err != nil {
				t.Fatalf("decode %s: %v", line, err)
			}
			if err := shape.match(v, "$", refs); err != nil {
				t.Errorf("GOEXPERIMENT=%s: %v\nJSON  %s\nshape %s", experiment, err, line, ts)
			}
		}
	}
	// Accepted types are written the same way by both implementations, byte for byte.
	if outputs[""] != outputs["nojsonv2"] {
		t.Errorf("the two implementations differ:\ndefault  %s\nnojsonv2 %s", outputs[""], outputs["nojsonv2"])
	}
}

// tsNode is a parsed TypeScript type in the subset tsType writes.
type tsNode struct {
	kind  string // string, number, boolean, null, array, empty, record, object, union, ref
	elem  *tsNode
	alts  []*tsNode
	props []tsProp
	name  string
}

type tsProp struct {
	name     string
	optional bool
	typ      *tsNode
}

func (n *tsNode) prop(name string) *tsNode {
	for _, p := range n.props {
		if p.name == name {
			return p.typ
		}
	}
	panic("no property " + name)
}

func parseTS(s string) (*tsNode, error) {
	p := &tsParser{s: s}
	n, err := p.union()
	if err == nil && p.i != len(s) {
		err = fmt.Errorf("trailing %q", s[p.i:])
	}
	return n, err
}

type tsParser struct {
	s string
	i int
}

func (p *tsParser) eat(tok string) bool {
	if strings.HasPrefix(p.s[p.i:], tok) {
		p.i += len(tok)
		return true
	}
	return false
}

func (p *tsParser) union() (*tsNode, error) {
	first, err := p.postfix()
	if err != nil {
		return nil, err
	}
	alts := []*tsNode{first}
	for p.eat(" | ") {
		n, err := p.postfix()
		if err != nil {
			return nil, err
		}
		alts = append(alts, n)
	}
	if len(alts) == 1 {
		return first, nil
	}
	return &tsNode{kind: "union", alts: alts}, nil
}

func (p *tsParser) postfix() (*tsNode, error) {
	var n *tsNode
	var err error
	if p.eat("(") {
		if n, err = p.union(); err != nil {
			return nil, err
		}
		if !p.eat(")") {
			return nil, fmt.Errorf("missing ) at %d", p.i)
		}
	} else if n, err = p.atom(); err != nil {
		return nil, err
	}
	for p.eat("[]") {
		n = &tsNode{kind: "array", elem: n}
	}
	return n, nil
}

func (p *tsParser) atom() (*tsNode, error) {
	for _, kw := range []string{"string", "number", "boolean", "null"} {
		if p.eat(kw) {
			return &tsNode{kind: kw}, nil
		}
	}
	switch {
	case p.eat("[]"):
		return &tsNode{kind: "empty"}, nil
	case p.eat("Record<string, "):
		elem, err := p.union()
		if err != nil {
			return nil, err
		}
		if !p.eat(">") {
			return nil, fmt.Errorf("missing > at %d", p.i)
		}
		return &tsNode{kind: "record", elem: elem}, nil
	case p.eat("{}"):
		return &tsNode{kind: "object"}, nil
	case p.eat("{ "):
		obj := &tsNode{kind: "object"}
		for {
			j := strings.Index(p.s[p.i:], ": ")
			if j < 0 {
				return nil, fmt.Errorf("missing property at %d", p.i)
			}
			name := p.s[p.i : p.i+j]
			p.i += j + 2
			prop := tsProp{name: strings.TrimSuffix(name, "?"), optional: strings.HasSuffix(name, "?")}
			if strings.HasPrefix(prop.name, `"`) {
				prop.name = strings.Trim(prop.name, `"`)
			}
			var err error
			if prop.typ, err = p.union(); err != nil {
				return nil, err
			}
			obj.props = append(obj.props, prop)
			if p.eat(" }") {
				return obj, nil
			}
			if !p.eat("; ") {
				return nil, fmt.Errorf("missing ; at %d", p.i)
			}
		}
	}
	j := p.i
	for j < len(p.s) && (p.s[j] == '_' || 'a' <= p.s[j]|0x20 && p.s[j]|0x20 <= 'z' || '0' <= p.s[j] && p.s[j] <= '9') {
		j++
	}
	if j == p.i {
		return nil, fmt.Errorf("unexpected %q", p.s[p.i:])
	}
	name := p.s[p.i:j]
	p.i = j
	return &tsNode{kind: "ref", name: name}, nil
}

// match checks a decoded JSON value against the shape. Numbers decode as float64.
func (n *tsNode) match(v any, path string, refs map[string]*tsNode) error {
	switch n.kind {
	case "union":
		var errs []string
		for _, alt := range n.alts {
			err := alt.match(v, path, refs)
			if err == nil {
				return nil
			}
			errs = append(errs, err.Error())
		}
		return fmt.Errorf("%s: no alternative matches: %s", path, strings.Join(errs, "; "))
	case "ref":
		return refs[n.name].match(v, path, refs)
	case "null":
		if v == nil {
			return nil
		}
	case "string":
		if _, ok := v.(string); ok {
			return nil
		}
	case "number":
		if _, ok := v.(float64); ok {
			return nil
		}
	case "boolean":
		if _, ok := v.(bool); ok {
			return nil
		}
	case "empty":
		if a, ok := v.([]any); ok && len(a) == 0 {
			return nil
		}
	case "array":
		if a, ok := v.([]any); ok {
			for i, e := range a {
				if err := n.elem.match(e, fmt.Sprintf("%s[%d]", path, i), refs); err != nil {
					return err
				}
			}
			return nil
		}
	case "record":
		if o, ok := v.(map[string]any); ok {
			for k, e := range o {
				if err := n.elem.match(e, path+"."+k, refs); err != nil {
					return err
				}
			}
			return nil
		}
	case "object":
		o, ok := v.(map[string]any)
		if !ok {
			break
		}
		seen := map[string]bool{}
		for _, p := range n.props {
			seen[p.name] = true
			e, present := o[p.name]
			if !present {
				if !p.optional {
					return fmt.Errorf("%s: property %q is missing but not optional", path, p.name)
				}
				continue
			}
			if err := p.typ.match(e, path+"."+p.name, refs); err != nil {
				return err
			}
		}
		for k := range o {
			if !seen[k] {
				return fmt.Errorf("%s: property %q is not in the shape", path, k)
			}
		}
		return nil
	}
	return fmt.Errorf("%s: %v (%T) is not %s", path, v, v, n.kind)
}
