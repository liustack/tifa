package analyze

import (
	"fmt"
	"strings"
	"unicode"
)

// segment is one segment of a Path: a literal, {name} or the trailing {name...}.
type segment struct {
	lit      string // literal text, empty for a parameter
	param    string // parameter name, empty for a literal
	wildcard bool   // {name...}
}

func (s segment) String() string {
	switch {
	case s.wildcard:
		return "{" + s.param + "...}"
	case s.param != "":
		return "{" + s.param + "}"
	}
	return s.lit
}

// parsePath checks a Path against the subset of ServeMux syntax that tifa accepts and splits it.
// Accepted: "/" for the exact home page, otherwise "/" followed by non-empty segments separated
// by "/", with no trailing "/". A segment is a literal, {name} or, as the last segment only,
// {name...}. name follows ServeMux: a Go identifier, keywords included, once per Path. Literals must not contain
// "{", "}", "$", "?", "#" or white space, because they would mean something else to ServeMux,
// TanStack Router or the URL. Methods, hosts and {$} are not accepted.
func parsePath(p string) ([]segment, error) {
	if p == "/" {
		return nil, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("Path %q must start with /", p)
	}
	if strings.HasSuffix(p, "/") {
		return nil, fmt.Errorf("Path %q must not end with a trailing /", p)
	}
	parts := strings.Split(p[1:], "/")
	segs := make([]segment, 0, len(parts))
	seen := map[string]bool{}
	for i, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("Path %q has an empty segment", p)
		}
		if !strings.HasPrefix(part, "{") {
			if j := strings.IndexAny(part, "{}$?# \t\r\n"); j >= 0 {
				return nil, fmt.Errorf("Path %q: segment %q must not contain %q", p, part, part[j])
			}
			segs = append(segs, segment{lit: part})
			continue
		}
		if !strings.HasSuffix(part, "}") {
			return nil, fmt.Errorf("Path %q: segment %q must be a whole {name} or {name...}", p, part)
		}
		name, wildcard := strings.CutSuffix(part[1:len(part)-1], "...")
		if !isWildcardName(name) {
			return nil, fmt.Errorf("Path %q: %q is not a valid parameter name", p, name)
		}
		if wildcard && i != len(parts)-1 {
			return nil, fmt.Errorf("Path %q: %s must be the last segment", p, part)
		}
		if seen[name] {
			return nil, fmt.Errorf("Path %q: parameter %q appears twice", p, name)
		}
		seen[name] = true
		segs = append(segs, segment{param: name, wildcard: wildcard})
	}
	return segs, nil
}

// isWildcardName is the parameter name rule of net/http ServeMux. Unlike token.IsIdentifier it
// accepts Go keywords such as type or range.
func isWildcardName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !unicode.IsLetter(c) && c != '_' && (i == 0 || !unicode.IsDigit(c)) {
			return false
		}
	}
	return true
}

func formatPath(segs []segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString("/")
		b.WriteString(s.String())
	}
	if b.Len() == 0 {
		return "/"
	}
	return b.String()
}

// commonPrefix returns the longest run of leading segments that all paths share. Segments are
// compared whole: equal literals, or parameters of equal name. {name...} never enters the prefix.
func commonPrefix(paths [][]segment) []segment {
	if len(paths) == 0 {
		return nil
	}
	var prefix []segment
	for i := 0; ; i++ {
		for _, p := range paths {
			if i >= len(p) || p[i].wildcard || p[i] != paths[0][i] {
				return prefix
			}
		}
		prefix = append(prefix, paths[0][i])
	}
}

// shape is the Path with parameter names erased, so two Paths that match the same URLs compare equal.
func shape(segs []segment) string {
	var b strings.Builder
	for _, s := range segs {
		switch {
		case s.wildcard:
			b.WriteString("/{...}")
		case s.param != "":
			b.WriteString("/{}")
		default:
			b.WriteString("/" + s.lit)
		}
	}
	return b.String()
}
