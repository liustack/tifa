package analyze

import (
	"fmt"
	"strings"
)

// parsePath reads a path in ServeMux syntax for the tests, checking it against the subset of ServeMux syntax that tifa accepts and splits it.
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
