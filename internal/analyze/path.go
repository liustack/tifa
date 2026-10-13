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

// relation is how the URL sets matched by two Paths relate, as in net/http's pattern comparison.
type relation int

const (
	equivalent   relation = iota // same URLs
	moreGeneral                  // the first matches a strict superset
	moreSpecific                 // the first matches a strict subset
	overlaps                     // some URLs in common, neither contains the other
	disjoint                     // no URL in common
)

// conflicts reports whether ServeMux (Go 1.22 semantics) would refuse to register both Paths:
// they match some URL in common and neither is more specific. It reimplements the comparison of
// net/http instead of registering into a ServeMux, because GODEBUG=httpmuxgo121=1 would switch a
// real ServeMux to the old literal semantics and the verdict would depend on the environment.
// Only the syntax parsePath accepts is handled: no methods, no hosts, no trailing slash.
func conflicts(p1, p2 []segment) bool {
	rel := comparePaths(muxSegments(p1), muxSegments(p2))
	return rel == equivalent || rel == overlaps
}

// muxSegments represents Path "/" the way the server registers it, as "/{$}", whose only segment
// is an end marker that matches the empty remainder. "/" never appears in a parsed literal.
func muxSegments(segs []segment) []segment {
	if len(segs) == 0 {
		return []segment{{lit: "/"}}
	}
	return segs
}

// comparePaths follows pattern.comparePaths in net/http.
func comparePaths(p1, p2 []segment) relation {
	multi1, multi2 := p1[len(p1)-1].wildcard, p2[len(p2)-1].wildcard
	if len(p1) != len(p2) && !multi1 && !multi2 {
		return disjoint
	}
	rel := equivalent
	s1, s2 := p1, p2
	for ; len(s1) > 0 && len(s2) > 0; s1, s2 = s1[1:], s2[1:] {
		rel = combine(rel, compareSegments(s1[0], s2[0]))
		if rel == disjoint {
			return rel
		}
	}
	switch {
	case len(s1) == 0 && len(s2) == 0:
		return rel
	case len(s1) < len(s2) && multi1:
		return combine(rel, moreGeneral)
	case len(s2) < len(s1) && multi2:
		return combine(rel, moreSpecific)
	}
	return disjoint
}

// compareSegments follows compareSegments in net/http.
func compareSegments(s1, s2 segment) relation {
	wild1, wild2 := s1.param != "" && !s1.wildcard, s2.param != "" && !s2.wildcard
	switch {
	case s1.wildcard && s2.wildcard:
		return equivalent
	case s1.wildcard:
		return moreGeneral
	case s2.wildcard:
		return moreSpecific
	case wild1 && wild2:
		return equivalent
	case wild1:
		if s2.lit == "/" { // a single wildcard does not match the end of the path
			return disjoint
		}
		return moreGeneral
	case wild2:
		if s1.lit == "/" {
			return disjoint
		}
		return moreSpecific
	case s1.lit == s2.lit:
		return equivalent
	}
	return disjoint
}

// combine follows combineRelationships in net/http.
func combine(r1, r2 relation) relation {
	switch r1 {
	case equivalent:
		return r2
	case disjoint:
		return disjoint
	case overlaps:
		if r2 == disjoint {
			return disjoint
		}
		return overlaps
	}
	// r1 is moreGeneral or moreSpecific
	switch r2 {
	case equivalent:
		return r1
	case inverse(r1):
		return overlaps
	}
	return r2
}

func inverse(r relation) relation {
	switch r {
	case moreGeneral:
		return moreSpecific
	case moreSpecific:
		return moreGeneral
	}
	return r
}
