package bundle

import (
	"path/filepath"
	"strings"
	"testing"
)

// flatReact installs react and react-dom side by side, as npm does.
func flatReact(t *testing.T, react, dom string) string {
	root := t.TempDir()
	writePackage(t, filepath.Join(root, "node_modules/react"), "react", react)
	writePackage(t, filepath.Join(root, "node_modules/react-dom"), "react-dom", dom)
	return root
}

func TestCheckReactPassesFromTheMinimumVersion(t *testing.T) {
	for _, version := range []string{MinReact, "19.3.1", "19.4.0", "20.0.0"} {
		if err := CheckReact(flatReact(t, version, version)); err != nil {
			t.Errorf("%s: %v", version, err)
		}
	}
}

func TestCheckReactFailsBelowTheMinimumVersion(t *testing.T) {
	for _, c := range []struct{ react, dom, old string }{
		{"19.2.4", "19.3.0", "react 19.2.4"},
		{"19.3.0", "19.2.7", "react-dom 19.2.7"},
		{"18.3.1", "18.3.1", "react 18.3.1"},
		{"19.3.0-canary-278794d7-20261002", "19.3.0", "react 19.3.0-canary-278794d7-20261002"},
	} {
		err := CheckReact(flatReact(t, c.react, c.dom))
		if err == nil {
			t.Errorf("%s with react-dom %s: no error", c.react, c.dom)
			continue
		}
		for _, want := range []string{c.old, MinReact + " or later", `"react": "` + MinReact + `"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s with react-dom %s: error %q does not say %q", c.react, c.dom, err, want)
			}
		}
	}
}

func TestCheckReactFailsOnAVersionItCannotRead(t *testing.T) {
	err := CheckReact(flatReact(t, "latest", "19.3.0"))
	if err == nil || !strings.Contains(err.Error(), `react version "latest"`) {
		t.Errorf("error = %v", err)
	}
}

func TestCheckReactFailsWithoutReactDOM(t *testing.T) {
	root := t.TempDir()
	writePackage(t, filepath.Join(root, "node_modules/react"), "react", MinReact)
	if err := CheckReact(root); err == nil || !strings.Contains(err.Error(), "react-dom is not installed") {
		t.Errorf("error = %v", err)
	}
}
