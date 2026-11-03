package scan

import (
	"fmt"
	"path"
	"strings"
)

// CheckMatch reports why pattern is no Match pattern, nil when it is one.
func CheckMatch(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("Match is empty: leave it out to take every entry")
	}
	for seg := range strings.SplitSeq(pattern, "/") {
		switch {
		case seg == "":
			return fmt.Errorf("Match %q has an empty segment: it is a path below the language folder, such as \"*/*/index.md\"", pattern)
		case seg == "**":
		case strings.Contains(seg, "**"):
			return fmt.Errorf("Match %q has ** inside a segment: ** stands for whole folders, such as \"**/index.md\"", pattern)
		default:
			if _, err := path.Match(seg, ""); err != nil {
				return fmt.Errorf("Match %q is not a pattern: %v", pattern, err)
			}
		}
	}
	return nil
}

// match reports whether the slash-separated path p matches pattern, whose ** segments match any
// number of folders.
func match(pattern, p string) bool {
	return matchSegs(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if matchSegs(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}
