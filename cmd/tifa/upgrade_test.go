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

var strictPins = map[string]bool{"@tanstack/react-router": true}

func TestSetPinsChangesOnlyThePinnedPackages(t *testing.T) {
	pins := map[string]string{
		"react": "19.3.0", "react-dom": "19.3.0", "vite": "8.3.4", "typescript": "6.0.3",
		"@tanstack/react-router": "1.170.46",
		"@vitejs/plugin-react":   "6.1.2", // the app does not have it: it is not added
	}
	got, changes, notes, err := setPins([]byte(appPackageJSON), pins, strictPins)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(
		`"1.100.0"`, `"1.170.46"`,
		`"react": "19.2.0",`, `"react": "19.3.0",`,
		`"5.9.0"`, `"6.0.3"`,
	).Replace(appPackageJSON)
	if string(got) != want {
		t.Errorf("package.json after:\n%s\nwant:\n%s", got, want)
	}
	wantChanges := []pinChange{
		{"@tanstack/react-router", "1.100.0", "1.170.46"},
		{"react", "19.2.0", "19.3.0"},
		{"typescript", "5.9.0", "6.0.3"},
	}
	if !reflect.DeepEqual(changes, wantChanges) {
		t.Errorf("changes %v, want %v", changes, wantChanges)
	}
	// react-dom is written as a range: it is not guessed at.
	if len(notes) != 1 || !strings.Contains(notes[0], "react-dom") || !strings.Contains(notes[0], "^19.2.0") || !strings.Contains(notes[0], "left as it is") {
		t.Errorf("notes %q, want one about react-dom ^19.2.0", notes)
	}
	// A second run changes nothing and says the same.
	again, changes, again2, err := setPins(got, pins, strictPins)
	if err != nil || string(again) != string(got) || len(changes) != 0 || !reflect.DeepEqual(again2, notes) {
		t.Errorf("second run: %v, %v, changed %v, notes %q", err, string(again) == string(got), changes, again2)
	}
}

// A package that is newer than the pin stays, with a line that says so. Only an exact version is
// compared: a range, a tag or a workspace: reference is left alone with a line. The router must
// equal the pin, so a newer one is set back, and a router written as a range is left to the user.
func TestSetPinsLeavesNewerAndUnclearVersions(t *testing.T) {
	pins := map[string]string{"react": "19.3.0", "vite": "8.3.4", "typescript": "6.0.3", "@tanstack/react-router": "1.170.46", "react-dom": "19.3.0"}
	for _, c := range []struct {
		name, in string
		change   bool   // the file changes
		note     string // text of the one note, "" for none
	}{
		{"newer", `"react": "19.4.0"`, false, "react 19.4.0 is newer than the 19.3.0 Tifa is tested with, left as it is"},
		{"newer prerelease of the next", `"react": "19.4.0-canary.1"`, false, "react 19.4.0-canary.1 is newer"},
		{"older prerelease", `"react": "19.3.0-canary.1"`, true, ""},
		{"caret", `"react": "^19.2.0"`, false, "react is written as ^19.2.0"},
		{"tilde", `"react": "~19.3.0"`, false, "react is written as ~19.3.0"},
		{"range", `"react": ">=19.2.0 <20"`, false, "react is written as >=19.2.0 <20"},
		{"workspace", `"react": "workspace:*"`, false, "react is written as workspace:*"},
		{"tag", `"react": "latest"`, false, "react is written as latest"},
		{"router newer is set back", `"@tanstack/react-router": "1.180.0"`, true, ""},
		{"router range", `"@tanstack/react-router": "^1.170.0"`, false, "@tanstack/react-router is written as ^1.170.0, and Tifa needs exactly 1.170.46: set it by hand"},
		{"same", `"react": "19.3.0"`, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := "{\n  \"dependencies\": {\n    " + c.in + "\n  }\n}\n"
			got, changes, notes, err := setPins([]byte(in), pins, strictPins)
			if err != nil {
				t.Fatal(err)
			}
			if changed := string(got) != in; changed != c.change || (len(changes) > 0) != c.change {
				t.Errorf("changed %v with changes %v, want changed %v:\n%s", changed, changes, c.change, got)
			}
			switch {
			case c.note == "" && len(notes) != 0:
				t.Errorf("notes %q, want none", notes)
			case c.note != "" && (len(notes) != 1 || !strings.Contains(notes[0], c.note)):
				t.Errorf("notes %q, want one with %q", notes, c.note)
			}
		})
	}
}

func TestSetPinsKeepsOtherFormatting(t *testing.T) {
	in := "{\r\n\t\"dependencies\" : { \"react\" :\"19.2.0\" ,\"x\":\"1\" }\r\n}"
	got, _, _, err := setPins([]byte(in), map[string]string{"react": "19.3.0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(in, "19.2.0", "19.3.0", 1); string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetPinsRefusesBadJSON(t *testing.T) {
	for _, in := range []string{``, `{"dependencies": [`, `[]`, `{"dependencies": {"react": 19}}`,
		`{"dependencies": {"react": "19.2.0"}`, `{"dependencies": {"react": "19.2.0"}} x`, `{"dependencies": {"react": "19.2.0"}} {}`, `{"dependencies": {"react": "19.2.0"}},`} {
		if _, _, _, err := setPins([]byte(in), map[string]string{"react": "19.3.0"}, nil); err == nil {
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
- Pages render faster. Upgrading: read the parameter in the ` + "`Load`" + `. See [docs](docs/start/app.md).
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
		{"v0.0.6", "read the parameter in the `Load`. See [docs](docs/start/app.md). It goes on in the next line."},
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

func readFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A workspace can give the app another tifa than go.mod names, and go get would write to the
// workspace's files. The command stops before it changes anything.
func TestUpgradeRefusesAWorkspace(t *testing.T) {
	t.Setenv("GOWORK", "")
	app := writeMod(t, "v0.0.5", toolLine)
	work := filepath.Join(filepath.Dir(app), "go.work")
	if err := os.WriteFile(work, []byte("go 1.27.0\n\nuse "+app+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(work) })
	before := readFile(t, filepath.Join(app, "go.mod"))
	_, _, err := run(t, "upgrade", "v0.0.6", "--dir", app)
	if err == nil || !strings.Contains(err.Error(), "go.work") || !strings.Contains(err.Error(), "GOWORK=off go tool tifa upgrade") {
		t.Fatalf("error %v, want one that names go.work and GOWORK=off go tool tifa upgrade", err)
	}
	if after := readFile(t, filepath.Join(app, "go.mod")); after != before {
		t.Errorf("go.mod changed:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(app, "go.sum")); err == nil {
		t.Error("go.sum was written")
	}
	// GOWORK=off lifts it: the command goes on to the version check, which refuses this one.
	t.Setenv("GOWORK", "off")
	if _, _, err := run(t, "upgrade", "v0.0.5", "--dir", app); err == nil || !strings.Contains(err.Error(), "already v0.0.5") {
		t.Errorf("with GOWORK=off: %v", err)
	}
}

// The step after go get takes the old version from --after, and refuses what is no version or not
// older than go.mod, before it changes a file.
func TestUpgradeAfterNeedsAnOlderVersion(t *testing.T) {
	app := writeMod(t, "v0.0.5", toolLine)
	if err := os.WriteFile(filepath.Join(app, "package.json"), []byte(appPackageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []string{"go.mod", "package.json"}
	before := map[string]string{}
	for _, f := range files {
		before[f] = readFile(t, filepath.Join(app, f))
	}
	for _, c := range []struct{ old, want string }{
		{"v0.0.5", "already v0.0.5"},
		{"v0.0.6", "v0.0.5 is older than v0.0.6"},
		{"0.0.4", `"0.0.4" is not a version`},
		{"latest", `"latest" is not a version`},
		{"v0.0", `"v0.0" is not a version`},
		{"v1", `"v1" is not a version`},
		{"v0.0.4+meta", "is not a version"},
		{" v0.0.4", "is not a version"},
	} {
		_, _, err := run(t, "upgrade", "--after", c.old, "--dir", app)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("--after %q: error %v, want one with %q", c.old, err, c.want)
		}
		for _, f := range files {
			if got := readFile(t, filepath.Join(app, f)); got != before[f] {
				t.Errorf("--after %q changed %s:\n%s", c.old, f, got)
			}
		}
	}
	if _, _, err := run(t, "upgrade", "v0.0.6", "--after", "v0.0.4", "--dir", app); err == nil || !strings.Contains(err.Error(), "takes no version") {
		t.Errorf("a version with --after: %v", err)
	}
}

// When a step after go get fails, the error says Go is at the new version and how to run the rest.
func TestUpgradeFinishTellsHowToResume(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "pnpm"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	app := writeMod(t, "v0.0.6", toolLine)
	if err := os.WriteFile(filepath.Join(app, "package.json"), []byte(appPackageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := run(t, "upgrade", "--after", "v0.0.5", "--dir", app)
	if err == nil {
		t.Fatal("no error although pnpm failed")
	}
	for _, want := range []string{"pnpm install failed", "Go is at v0.0.6", "cd " + cdOperand(app) + " && go tool tifa upgrade --after v0.0.5"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q has no %q", err, want)
		}
	}
	if n := strings.Count(err.Error(), "\n"); n != 1 {
		t.Errorf("error has %d line breaks, want the cause and one line on how to go on:\n%s", n, err)
	}
}

// The entry about tifa upgrade in the changelog must not hold the literal that the command reads
// as a migration note: it would be printed to every app that upgrades past it.
func TestChangelogEntriesAboutUpgradeAreNoNotes(t *testing.T) {
	data := readFile(t, filepath.Join("..", "..", "CHANGELOG.md"))
	data = strings.Replace(data, "## [Unreleased]", "## [0.0.9999] - 2099-01-01", 1)
	for _, n := range upgradingNotes(data, "v0.0.16", "v0.0.9999") {
		if strings.Contains(strings.ToLower(n.Text), "upgrade") {
			t.Errorf("the changelog gives the note %q for %s: write the word without its colon in the entry", n.Text, n.Version)
		}
	}
}
