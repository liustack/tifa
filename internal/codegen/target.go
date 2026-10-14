package codegen

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	esVersion = regexp.MustCompile(`^(?:es5|es6|es20(?:1[5-9]|2[0-9])|esnext)$`)
	browser   = regexp.MustCompile(`^(?:chrome|edge|firefox|ios|opera|safari)[0-9]+(?:\.[0-9]+)*$`)
)

// viteTarget turns build.target into Vite's build.target: a comma-separated list of one
// ECMAScript version and browsers with versions, such as "es2020" or "es2019,chrome90,safari14",
// becomes a list of the same names. The configuration never leaves build.target empty.
func viteTarget(target string) ([]string, error) {
	var list []string
	versions := 0
	for _, item := range strings.Split(target, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		switch {
		case esVersion.MatchString(item):
			versions++
		case !browser.MatchString(item):
			return nil, fmt.Errorf("build.target: %q is neither an ECMAScript version (es2020, esnext) nor a browser with a version (chrome90, safari14)", item)
		}
		list = append(list, item)
	}
	if versions > 1 {
		return nil, fmt.Errorf("build.target: %q names more than one ECMAScript version", target)
	}
	return list, nil
}
