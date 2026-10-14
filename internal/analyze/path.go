package analyze

import (
	"fmt"
	"net/url"
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

// dirSegments turns the directory of a route or Layout, such as "app/projects/project+", into
// its path segments: every directory below app/ is one segment. A name ending in ++ is the rest
// of the path {name...}, allowed only as the last segment, a name ending in + is the parameter
// {name}, any other name is a literal. A parameter name follows ServeMux and appears once per
// path. A literal holds only ASCII letters, digits and - . _ ~, the characters that mean the same
// to ServeMux, the browser router and a URL, and that the go command accepts in an import path.
func dirSegments(dir string) ([]segment, error) {
	rest, _ := strings.CutPrefix(dir, "app")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return nil, nil
	}
	parts := strings.Split(rest, "/")
	segs := make([]segment, 0, len(parts))
	seen := map[string]bool{}
	for i, part := range parts {
		name, wildcard := strings.CutSuffix(part, "++")
		param := wildcard
		if !wildcard {
			name, param = strings.CutSuffix(part, "+")
		}
		if !param {
			if j := strings.IndexFunc(part, func(r rune) bool { return !isLiteralRune(r) }); j >= 0 {
				return nil, fmt.Errorf("directory %q holds %q: a directory name below app/ is a URL segment of letters, digits, - . _ and ~, or name+ for a parameter and name++ for the rest of the path", part, part[j:j+1])
			}
			segs = append(segs, segment{lit: part})
			continue
		}
		if !isWildcardName(name) {
			return nil, fmt.Errorf("directory %q: %q before the + is not a valid parameter name, which starts with a letter or _ followed by letters, digits or _", part, name)
		}
		if wildcard && i != len(parts)-1 {
			return nil, fmt.Errorf("directory %q matches the rest of the path, so no route or Layout can be below it", part)
		}
		if seen[name] {
			return nil, fmt.Errorf("directory %q: parameter %q appears twice in %s", part, name, dir)
		}
		seen[name] = true
		segs = append(segs, segment{param: name, wildcard: wildcard})
	}
	return segs, nil
}

func isLiteralRune(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || strings.ContainsRune("-._~", r)
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

// relation is how the URL sets matched by two Paths relate, as in net/http's pattern comparison.
type relation int

const (
	equivalent   relation = iota // same URLs
	moreGeneral                  // the first matches a strict superset
	moreSpecific                 // the first matches a strict subset
	overlaps                     // some URLs in common, neither contains the other
	disjoint                     // no URL in common
)

// muxSegments represents Path "/" the way the server registers it, as "/{$}", whose only segment
// is an end marker that matches the empty remainder. "/" never appears in a parsed literal.
func muxSegments(segs []segment) []segment {
	if len(segs) == 0 {
		return []segment{{lit: "/"}}
	}
	return segs
}

// comparePaths relates two Paths the way pattern.comparePaths in net/http (Go 1.22 semantics)
// does. ServeMux refuses to register two Paths whose relation is equivalent or overlaps.
// The analyzer reimplements the comparison instead of registering into a ServeMux, because
// GODEBUG=httpmuxgo121=1 switches a real ServeMux to the old literal semantics and the verdict
// would depend on the environment. Only the syntax dirSegments produces is handled: no methods,
// no hosts, no trailing slash. Pass the segments through muxSegments first.
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
	}
	// ServeMux compares literals after unescaping them, so "/%61" is "/a", and "/%2F" ends in the
	// same "/" literal that stands for {$}.
	l1, l2 := pathUnescape(s1.lit), pathUnescape(s2.lit)
	switch {
	case wild1:
		if l2 == "/" { // a single wildcard does not match the end of the path
			return disjoint
		}
		return moreGeneral
	case wild2:
		if l1 == "/" {
			return disjoint
		}
		return moreSpecific
	case l1 == l2:
		return equivalent
	}
	return disjoint
}

// pathUnescape is pathUnescape of net/http: an invalid escape leaves the literal as it is.
func pathUnescape(lit string) string {
	u, err := url.PathUnescape(lit)
	if err != nil {
		return lit
	}
	return u
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
