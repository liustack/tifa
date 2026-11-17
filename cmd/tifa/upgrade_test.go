package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/bundle"
)

func TestCheckUpgradeVersions(t *testing.T) {
	for _, c := range []struct {
		current, target string
		want            string // text the error holds, "" for none
	}{
		{"v0.0.4", "v0.0.5", ""},
		{"v0.0.9", "v0.0.10", ""},
		{"v0.0.4", "v0.1.0", ""},
		{"v0.0.4", "v0.0.4", "already v0.0.4"},
		{"v0.0.5", "v0.0.4", "older than v0.0.5"},
		{"v0.0.10", "v0.0.9", "older than v0.0.10"},
		{"v0.0.4", "v0.0.5-rc.1", ""},
		{"v0.0.5", "v0.0.5-rc.1", "older than v0.0.5"},
		{"v0.0.4", "0.0.5", "not a version"},
		{"v0.0.4", "main", "not a version"},
		{"v0.0.4", "v1.2", "not a version"},
		{"v0.0.4", "v0.0.5+incompatible", "not a version"},
		{"v0.0.0-20261117-abc", "v0.0.5", ""},
	} {
		err := checkUpgradeVersions(c.current, c.target)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s to %s: %v", c.current, c.target, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s to %s: error %v, want one with %q", c.current, c.target, err, c.want)
		}
	}
}

const appPackageJSON = `{
  "name": "shop",
  "private": true,
  "scripts": {
    "react": "echo react 1.0.0",
    "prepare": "go tool tifa gen"
  },
  "dependencies": {
    "@tanstack/react-router": "1.100.0",
    "react": "19.2.0",
    "react-dom": "^19.2.0",
    "zod": "3.0.0"
  },
  "devDependencies": {
    "typescript": "5.9.0",
    "vite": "8.3.4"
  },
  "pnpm": {
    "overrides": { "react": "19.2.0" }
  }
}
`

func TestSetPinsChangesOnlyThePinnedPackages(t *testing.T) {
	pins := map[string]string{
		"react": "19.3.0", "react-dom": "19.3.0", "vite": "8.3.4", "typescript": "6.0.3",
		"@tanstack/react-router": "1.170.46",
		"@vitejs/plugin-react":   "6.1.2", // the app does not have it: it is not added
	}
	got, changes, err := setPins([]byte(appPackageJSON), pins)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(
		`"1.100.0"`, `"1.170.46"`,
		`"react": "19.2.0",`, `"react": "19.3.0",`,
		`"^19.2.0"`, `"19.3.0"`,
		`"5.9.0"`, `"6.0.3"`,
	).Replace(appPackageJSON)
	if string(got) != want {
		t.Errorf("package.json after:\n%s\nwant:\n%s", got, want)
	}
	wantChanges := []pinChange{
		{"@tanstack/react-router", "1.100.0", "1.170.46"},
		{"react", "19.2.0", "19.3.0"},
		{"react-dom", "^19.2.0", "19.3.0"},
		{"typescript", "5.9.0", "6.0.3"},
	}
	if !reflect.DeepEqual(changes, wantChanges) {
		t.Errorf("changes %v, want %v", changes, wantChanges)
	}
	// A second run changes nothing.
	again, changes, err := setPins(got, pins)
	if err != nil || string(again) != string(got) || len(changes) != 0 {
		t.Errorf("second run: %v, %v, changed %v", err, string(again) == string(got), changes)
	}
}

func TestSetPinsKeepsOtherFormatting(t *testing.T) {
	in := "{\r\n\t\"dependencies\" : { \"react\" :\"19.2.0\" ,\"x\":\"1\" }\r\n}"
	got, _, err := setPins([]byte(in), map[string]string{"react": "19.3.0"})
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(in, "19.2.0", "19.3.0", 1); string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetPinsRefusesBadJSON(t *testing.T) {
	for _, in := range []string{``, `{"dependencies": [`, `[]`, `{"dependencies": {"react": 19}}`} {
		if _, _, err := setPins([]byte(in), map[string]string{"react": "19.3.0"}); err == nil {
			t.Errorf("%q: no error", in)
		}
	}
}

// The pins are what tifa new writes: the same numbers, read from the same template.
func TestPinnedPackagesAreWhatNewWrites(t *testing.T) {
	pins, err := pinnedPackages()
	if err != nil {
		t.Fatal(err)
	}
	if pins["@tanstack/react-router"] != bundle.TestedReactRouter {
		t.Errorf("router %q, want %q", pins["@tanstack/react-router"], bundle.TestedReactRouter)
	}
	for _, name := range []string{"react", "react-dom", "@types/react", "@types/react-dom", "@vitejs/plugin-react", "typescript", "vite"} {
		if pins[name] == "" {
			t.Errorf("no pin for %s in %v", name, pins)
		}
	}
	if _, ok := pins["name"]; ok || len(pins) != 8 {
		t.Errorf("pins %v: want the 8 packages of the template", pins)
	}
}

const sampleChangelog = `# Changelog

Intro with Upgrading: in prose that is no entry.

## [Unreleased]

- Upgrading: not released yet.

## [0.0.6] - 2026-11-20

### Changed

- Plain change.
- Pages render faster. Upgrading: read the parameter in the ` + "`Load`" + `. See [docs](docs/app.md).
  It goes on in the next line.
- Another one. Upgrading from v0.0.4: rename ` + "`A`" + ` to ` + "`B`" + `.

## [0.0.5] - 2026-11-18

### Fixed

- Fix. Upgrading: run ` + "`go tool tifa agents`" + `.
- Nothing to do here.

## [0.0.4] - 2026-11-17

- Old. Upgrading: not for you, you are on v0.0.4.
`

func TestUpgradingNotes(t *testing.T) {
	got := upgradingNotes(sampleChangelog, "v0.0.4", "v0.0.6")
	want := []upgradeNote{
		{"v0.0.6", "read the parameter in the `Load`. See [docs](docs/app.md). It goes on in the next line."},
		{"v0.0.6", "rename `A` to `B`."},
		{"v0.0.5", "run `go tool tifa agents`."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes %q, want %q", got, want)
	}
	// The new version only, and nothing from the version you are on or from Unreleased.
	if got := upgradingNotes(sampleChangelog, "v0.0.5", "v0.0.5"); len(got) != 0 {
		t.Errorf("same version: %q", got)
	}
	if got := upgradingNotes(sampleChangelog, "v0.0.5", "v0.0.6"); len(got) != 2 || got[0].Version != "v0.0.6" {
		t.Errorf("v0.0.5 to v0.0.6: %q", got)
	}
	// The order is newest first, as the changelog is. A version missing from the changelog is skipped.
	if got := upgradingNotes(sampleChangelog, "v0.0.3", "v0.0.5"); len(got) != 2 || got[1].Version != "v0.0.4" {
		t.Errorf("v0.0.3 to v0.0.5: %q", got)
	}
}

// A refusal comes before anything is changed: go.mod stays as it is.
func TestUpgradeRefuses(t *testing.T) {
	app := writeMod(t, "v0.0.5", toolLine)
	replaced := writeMod(t, "v0.0.5", toolLine+"replace github.com/liustack/tifa => ../tifa\n")
	noTifa := t.TempDir()
	if err := os.WriteFile(filepath.Join(noTifa, "go.mod"), []byte("module example.com/x\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, dir, version, want string
	}{
		{"same", app, "v0.0.5", "already v0.0.5"},
		{"older", app, "v0.0.4", "older than v0.0.5"},
		{"not a version", app, "latest", "not a version"},
		{"replaced", replaced, "v0.0.6", "replaces " + tifaModule},
		{"not a tifa app", noTifa, "v0.0.6", "not a Tifa app"},
		{"no go.mod", t.TempDir(), "v0.0.6", "no go.mod"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before, _ := os.ReadFile(filepath.Join(c.dir, "go.mod"))
			_, _, err := run(t, "upgrade", c.version, "--dir", c.dir)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want one with %q", err, c.want)
			}
			if after, _ := os.ReadFile(filepath.Join(c.dir, "go.mod")); string(after) != string(before) {
				t.Errorf("go.mod changed:\n%s", after)
			}
		})
	}
}

// The step after go get checks the version it is told the app had.
func TestUpgradeAfterNeedsAnOlderVersion(t *testing.T) {
	app := writeMod(t, "v0.0.5", toolLine)
	for _, old := range []string{"v0.0.5", "v0.0.6", "0.0.4"} {
		if _, _, err := run(t, "upgrade", "--after", old, "--dir", app); err == nil {
			t.Errorf("--after %s: no error", old)
		}
	}
	if _, _, err := run(t, "upgrade", "v0.0.6", "--after", "v0.0.4", "--dir", app); err == nil {
		t.Error("a version with --after: no error")
	}
}
