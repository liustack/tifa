package bundle

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// MinReact is the oldest React and React DOM tifa builds with. The Vite plugin rewrites the
// string renderer of React DOM as 19.3 writes it (see js/vite/plugin.js). The package.json that
// tifa new writes installs this version, which a test in cmd/tifa checks.
const MinReact = "19.3.0"

// CheckReact returns an error when the app at root installs a react or react-dom older than
// MinReact, a prerelease of it included. It looks the packages up the way Node and Vite do, from
// the real directory of the app.
func CheckReact(root string) error {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, name := range []string{"react", "react-dom"} {
		_, version, err := findPackage(root, name)
		if err != nil {
			return err
		}
		old, err := olderThan(version, MinReact)
		if err != nil {
			return fmt.Errorf("%s version %q: %w", name, version, err)
		}
		if old {
			return fmt.Errorf(`the app installs %s %s, and tifa needs react and react-dom %s or later.
Write "react": "%s" and "react-dom": "%s" in package.json and install again`,
				name, version, MinReact, MinReact, MinReact)
		}
	}
	return nil
}

// olderThan reports whether the version is older than min, both of the form major.minor.patch.
// A prerelease of a version, such as 19.3.0-canary-278794d7-20261002, is older than it.
func olderThan(version, min string) (bool, error) {
	v, pre, err := parseVersion(version)
	if err != nil {
		return false, err
	}
	m, _, err := parseVersion(min)
	if err != nil {
		return false, err
	}
	for i := range v {
		if v[i] != m[i] {
			return v[i] < m[i], nil
		}
	}
	return pre, nil
}

func parseVersion(version string) ([3]int, bool, error) {
	var v [3]int
	core, _, pre := strings.Cut(version, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false, fmt.Errorf("not of the form major.minor.patch")
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return v, false, fmt.Errorf("not of the form major.minor.patch")
		}
		v[i] = n
	}
	return v, pre, nil
}
