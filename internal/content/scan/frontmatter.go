package scan

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// matter is what one Markdown file holds: the keys tifa reads, the front matter as the JSON of the
// schema, and the body.
type matter struct {
	date    time.Time
	draft   bool
	slug    string
	slugAt  pos // zero without a slug
	group   string
	groupAt pos // zero without a translationKey
	data    []byte
	body    []byte
	errs    []posError
}

// pos is a line and column of a file, both from 1.
type pos struct{ line, col int }

type posError struct {
	at  pos
	msg string
}

// frameworkKeys are the keys of the front matter tifa reads itself.
const (
	keyDate  = "date"
	keyDraft = "draft"
	keySlug  = "slug"
	keyGroup = "translationKey"
)

// setByTifa are the json names of content.Entry that tifa works out and front matter cannot set.
var setByTifa = map[string]bool{"id": true, "lang": true, "path": true, "translations": true, "words": true}

// parseMatter reads src, a Markdown file, against the schema tree of the type named typeName.
func parseMatter(src []byte, schema *Node, typeName string) *matter {
	m := &matter{}
	src = bytes.TrimPrefix(src, []byte{0xEF, 0xBB, 0xBF}) // a byte order mark
	first, rest, _ := bytes.Cut(src, []byte("\n"))
	if string(bytes.TrimRight(first, "\r")) != "---" {
		m.errs = append(m.errs, posError{pos{1, 1}, fmt.Sprintf("no front matter: an entry starts with --- and the keys of %s", typeName)})
		return m
	}
	// The front matter is the lines up to the next line that is ---.
	var yamlText []byte
	lines := 1
	found := false
	for len(rest) > 0 {
		line, next, _ := bytes.Cut(rest, []byte("\n"))
		lines++
		if string(bytes.TrimRight(line, "\r")) == "---" {
			found = true
			m.body = next
			break
		}
		yamlText = append(yamlText, line...)
		yamlText = append(yamlText, '\n')
		rest = next
	}
	if !found {
		m.errs = append(m.errs, posError{pos{1, 1}, "the front matter has no line --- after it"})
		return m
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(yamlText, &doc); err != nil {
		m.errs = append(m.errs, yamlError(err))
		return m
	}
	c := &converter{m: m, typeName: typeName}
	top := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: 1, Column: 1}
	if len(doc.Content) > 0 {
		top = resolve(doc.Content[0])
	}
	if top.Kind != yaml.MappingNode {
		c.errorAt(top, "the front matter must be keys and values, such as title: Hello")
		return m
	}
	m.data = c.object(top, schema, "", true)
	return m
}

var yamlLine = regexp.MustCompile(`^yaml: line (\d+): (.*)$`)

// yamlError places an error of the YAML parser in the file: line 1 of the front matter is line 2.
func yamlError(err error) posError {
	msg := err.Error()
	if sub := yamlLine.FindStringSubmatch(msg); sub != nil {
		n, _ := strconv.Atoi(sub[1])
		return posError{pos{n + 1, 0}, "the front matter is not YAML: " + sub[2]}
	}
	return posError{pos{1, 1}, "the front matter is not YAML: " + strings.TrimPrefix(msg, "yaml: ")}
}

// resolve follows an alias to the node it names.
func resolve(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

type converter struct {
	m        *matter
	typeName string
}

// at is the position of n in the file: the front matter starts on line 2.
func at(n *yaml.Node) pos { return pos{n.Line + 1, n.Column} }

func (c *converter) errorAt(n *yaml.Node, format string, args ...any) {
	c.m.errs = append(c.m.errs, posError{at(n), fmt.Sprintf(format, args...)})
}

// object converts the mapping n into a JSON object of the struct t. top is true for the front
// matter itself, where tifa's keys live.
func (c *converter) object(n *yaml.Node, t *Node, path string, top bool) []byte {
	values := map[string][]byte{}
	seen := map[string]int{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := resolve(n.Content[i]), resolve(n.Content[i+1])
		if k.Kind != yaml.ScalarNode {
			c.errorAt(k, "a key of the front matter must be a name, such as title")
			continue
		}
		key := k.Value
		if line, ok := seen[key]; ok {
			c.errorAt(k, "%q is written twice, first on line %d", key, line)
			continue
		}
		seen[key] = at(k).line
		if top {
			switch {
			case key == keyDate:
				if d, ok := c.date(v, key); ok {
					c.m.date = d
				}
				continue
			case key == keyDraft:
				if v.ShortTag() != "!!bool" {
					c.errorAt(v, "draft: want true or false, got %s", c.got(v, true))
					continue
				}
				c.m.draft, _ = strconv.ParseBool(v.Value)
				continue
			case key == keySlug:
				s, ok := c.name(v, key)
				if !ok {
					continue
				}
				if why := segmentProblem(s); why != "" {
					c.errorAt(v, "slug: %q is not one URL segment: %s", s, why)
					continue
				}
				c.m.slug, c.m.slugAt = s, at(v)
				continue
			case key == keyGroup:
				s, ok := c.name(v, key)
				if !ok {
					continue
				}
				if s == "" {
					c.errorAt(v, "translationKey is empty: leave it out to link the translations by ID")
					continue
				}
				c.m.group, c.m.groupAt = s, at(v)
				continue
			case setByTifa[key]:
				c.errorAt(k, "%q is set by tifa and cannot be written in front matter", key)
				continue
			}
		}
		f := t.field(key)
		if f == nil {
			c.errorAt(k, "%s", c.unknown(key, t, path))
			continue
		}
		if v.ShortTag() == "!!null" && !f.Required && f.Node.Kind != Pointer && f.Node.Kind != Slice && f.Node.Kind != Map {
			continue // an optional field with no value keeps its zero value
		}
		if out, ok := c.value(v, f.Node, join(path, key)); ok {
			values[key] = out
		}
	}
	out := []byte{'{'}
	for _, f := range t.Fields {
		v, ok := values[f.Key]
		if !ok {
			if _, written := seen[f.Key]; !written && f.Required {
				if top {
					c.m.errs = append(c.m.errs, posError{pos{1, 1}, fmt.Sprintf("front matter has no %q, which %s needs. Make the field a pointer or add omitempty to make it optional", f.Key, f.Go)})
				} else {
					c.errorAt(n, "%s has no %q, which %s needs. Make the field a pointer or add omitempty to make it optional", path, f.Key, f.Go)
				}
			}
			continue
		}
		if len(out) > 1 {
			out = append(out, ',')
		}
		out, _ = jsontext.AppendQuote(out, f.Key)
		out = append(out, ':')
		out = append(out, v...)
	}
	return append(out, '}')
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// unknown is the message for a key that is no field of the struct t.
func (c *converter) unknown(key string, t *Node, path string) string {
	owner := c.typeName
	where := "a field of " + owner
	if path != "" {
		where = "a key of " + path
	}
	msg := fmt.Sprintf("%q is not %s.", key, where)
	keys := t.Keys()
	if near := nearest(key, keys); near != "" {
		msg += fmt.Sprintf(" Did you mean %q?", near)
	}
	if len(keys) == 0 {
		return msg + " It has no fields"
	}
	return msg + " Fields: " + strings.Join(keys, ", ")
}

// nearest returns the key of keys closest to key, "" when none is close.
func nearest(key string, keys []string) string {
	best, bestDist := "", 3
	for _, k := range keys {
		if strings.EqualFold(k, key) {
			return k
		}
		if d := distance(strings.ToLower(key), strings.ToLower(k)); d < bestDist {
			best, bestDist = k, d
		}
	}
	return best
}

// distance is the Levenshtein distance of a and b, by runes.
func distance(a, b string) int {
	x, y := []rune(a), []rune(b)
	prev := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(x); i++ {
		cur := make([]int, len(y)+1)
		cur[0] = i
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(y)]
}

// name reads a scalar that names something, a slug or a translation key. Numbers are names too.
func (c *converter) name(v *yaml.Node, key string) (string, bool) {
	switch v.ShortTag() {
	case "!!str", "!!int", "!!float":
		return v.Value, true
	}
	c.errorAt(v, "%s: want a string, got %s", key, c.got(v, true))
	return "", false
}

// segmentProblem says why s is not one URL segment, "" when it is.
func segmentProblem(s string) string {
	switch {
	case s == "":
		return "it is empty"
	case s == "." || s == "..":
		return "it is . or .."
	}
	for _, r := range s {
		switch {
		case strings.ContainsRune("/?#%", r):
			return "it holds " + string(r)
		case unicode.IsSpace(r):
			return "it holds white space"
		case unicode.IsControl(r):
			return "it holds a control character"
		}
	}
	return ""
}

// date reads a date: 2006-01-02, which is midnight UTC, or RFC 3339.
func (c *converter) date(v *yaml.Node, path string) (time.Time, bool) {
	tag := v.ShortTag()
	if tag != "!!timestamp" && tag != "!!str" {
		c.errorAt(v, "%s: want a date, got %s", path, c.got(v, false))
		return time.Time{}, false
	}
	if d, err := time.Parse(time.DateOnly, v.Value); err == nil {
		return d, true
	}
	if d, err := time.Parse(time.RFC3339Nano, v.Value); err == nil {
		return d, true
	}
	c.errorAt(v, "%s: %q is not a date: want 2006-01-02 or RFC 3339 such as 2026-11-20T09:00:00+08:00", path, v.Value)
	return time.Time{}, false
}

// value converts n into the JSON of t. path names it in messages.
func (c *converter) value(n *yaml.Node, t *Node, path string) ([]byte, bool) {
	tag := n.ShortTag()
	if tag == "!!null" {
		switch t.Kind {
		case Pointer, Slice, Map:
			return []byte("null"), true
		}
		c.errorAt(n, "%s: want %s, got nothing", path, want(t))
		return nil, false
	}
	switch t.Kind {
	case Pointer:
		return c.value(n, t.Elem, path)
	case String:
		if tag != "!!str" {
			if n.Kind == yaml.ScalarNode {
				c.errorAt(n, "%s: want a string, got %s: write it in quotes", path, c.got(n, true))
			} else {
				c.errorAt(n, "%s: want a string, got %s", path, c.got(n, false))
			}
			return nil, false
		}
		out, err := jsontext.AppendQuote(nil, n.Value)
		if err != nil {
			c.errorAt(n, "%s: %v", path, err)
			return nil, false
		}
		return out, true
	case Bool:
		if tag != "!!bool" {
			c.errorAt(n, "%s: want true or false, got %s", path, c.got(n, true))
			return nil, false
		}
		b, _ := strconv.ParseBool(n.Value)
		return strconv.AppendBool(nil, b), true
	case Int, Uint, Float:
		return c.number(n, t, path)
	case Time:
		d, ok := c.date(n, path)
		if !ok {
			return nil, false
		}
		return strconv.AppendQuote(nil, d.Format(time.RFC3339Nano)), true
	case Slice:
		if n.Kind != yaml.SequenceNode {
			c.errorAt(n, "%s: want %s, got %s", path, want(t), c.got(n, false))
			return nil, false
		}
		out, ok := []byte{'['}, true
		for i, item := range n.Content {
			v, good := c.value(resolve(item), t.Elem, fmt.Sprintf("%s[%d]", path, i))
			ok = ok && good
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, v...)
		}
		return append(out, ']'), ok
	case Map:
		if n.Kind != yaml.MappingNode {
			c.errorAt(n, "%s: want %s, got %s", path, want(t), c.got(n, false))
			return nil, false
		}
		out, ok := []byte{'{'}, true
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := resolve(n.Content[i]), resolve(n.Content[i+1])
			if k.Kind != yaml.ScalarNode {
				c.errorAt(k, "%s: a key must be a name", path)
				ok = false
				continue
			}
			if seen[k.Value] {
				c.errorAt(k, "%s: %q is written twice", path, k.Value)
				ok = false
				continue
			}
			seen[k.Value] = true
			val, good := c.value(v, t.Elem, join(path, k.Value))
			ok = ok && good
			if len(out) > 1 {
				out = append(out, ',')
			}
			out, _ = jsontext.AppendQuote(out, k.Value)
			out = append(out, ':')
			out = append(out, val...)
		}
		return append(out, '}'), ok
	case Struct:
		if n.Kind != yaml.MappingNode {
			c.errorAt(n, "%s: want %s, got %s", path, want(t), c.got(n, false))
			return nil, false
		}
		before := len(c.m.errs)
		out := c.object(n, t, path, false)
		return out, len(c.m.errs) == before
	}
	panic(fmt.Sprintf("scan: no conversion for kind %d", t.Kind))
}

// number converts a YAML number into the JSON of an Int, Uint or Float of t.Bits.
func (c *converter) number(n *yaml.Node, t *Node, path string) ([]byte, bool) {
	tag := n.ShortTag()
	if tag != "!!int" && (tag != "!!float" || t.Kind != Float) {
		c.errorAt(n, "%s: want %s, got %s", path, want(t), c.got(n, true))
		return nil, false
	}
	switch t.Kind {
	case Int:
		var i int64
		if err := n.Decode(&i); err != nil {
			c.errorAt(n, "%s: %s is out of range: want %s", path, n.Value, want(t))
			return nil, false
		}
		lo, hi := -int64(1)<<(t.Bits-1), int64(1)<<(t.Bits-1)-1
		if i < lo || i > hi {
			c.errorAt(n, "%s: %s is out of range: want an integer from %d to %d", path, n.Value, lo, hi)
			return nil, false
		}
		return strconv.AppendInt(nil, i, 10), true
	case Uint:
		var u uint64
		if err := n.Decode(&u); err != nil {
			c.errorAt(n, "%s: %s is out of range: want %s", path, n.Value, want(t))
			return nil, false
		}
		if t.Bits < 64 && u > uint64(1)<<t.Bits-1 {
			c.errorAt(n, "%s: %s is out of range: want an integer from 0 to %d", path, n.Value, uint64(1)<<t.Bits-1)
			return nil, false
		}
		return strconv.AppendUint(nil, u, 10), true
	}
	var f float64
	if err := n.Decode(&f); err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		c.errorAt(n, "%s: %s is not a number JSON can hold", path, n.Value)
		return nil, false
	}
	if t.Bits == 32 && math.Abs(f) > math.MaxFloat32 {
		c.errorAt(n, "%s: %s is out of range for a float32", path, n.Value)
		return nil, false
	}
	return strconv.AppendFloat(nil, f, 'g', -1, t.Bits), true
}

// want describes what t takes.
func want(t *Node) string {
	switch t.Kind {
	case String:
		return "a string"
	case Bool:
		return "true or false"
	case Int:
		return "an integer"
	case Uint:
		return "an integer of 0 or more"
	case Float:
		return "a number"
	case Time:
		return "a date"
	case Pointer:
		return want(t.Elem)
	case Slice:
		return "a list of " + plural(t.Elem)
	case Map:
		return "a map of " + plural(t.Elem)
	}
	return "a map of keys and values"
}

func plural(t *Node) string {
	switch t.Kind {
	case String:
		return "strings"
	case Bool:
		return "true or false"
	case Int, Uint:
		return "integers"
	case Float:
		return "numbers"
	case Time:
		return "dates"
	case Pointer:
		return plural(t.Elem)
	case Slice:
		return "lists"
	}
	return "maps"
}

// got describes n. A scalar is quoted when quote is true, so that the message shows what was
// written.
func (c *converter) got(n *yaml.Node, quote bool) string {
	if n.Kind == yaml.ScalarNode && quote {
		return strconv.Quote(n.Value)
	}
	switch n.Kind {
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "a map"
	}
	switch n.ShortTag() {
	case "!!str":
		return "a string"
	case "!!int", "!!float":
		return "a number"
	case "!!bool":
		return "true or false"
	case "!!timestamp":
		return "a date"
	case "!!null":
		return "nothing"
	}
	return strconv.Quote(n.Value)
}
