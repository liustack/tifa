package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/bundle"
	"golang.org/x/mod/semver"
)

// checkUpgradeVersions says why an app on current cannot move to target: target is no version, or
// it is the one the app has, or an older one. Versions are compared as semantic versions, so v0.0.10
// is newer than v0.0.9.
func checkUpgradeVersions(current, target string) error {
	if semver.Canonical(target) != target {
		return fmt.Errorf("%q is not a version: write it as v0.0.17, or leave it out for the latest release", target)
	}
	switch semver.Compare(target, current) {
	case 0:
		return fmt.Errorf("the app is already %s, there is nothing to upgrade", current)
	case -1:
		return fmt.Errorf("%s is older than %s, which the app has. tifa upgrade only moves forward", target, current)
	}
	return nil
}

// pinnedPackages returns the npm packages that tifa new writes with an exact version, and those
// versions, read from the template of package.json so that the two cannot differ.
func pinnedPackages() (map[string]string, error) {
	text, err := renderFile(newapp, "newapp/package.json.tmpl", newApp{NPM: "app", ReactRouter: bundle.TestedReactRouter})
	if err != nil {
		return nil, err
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(text, &pkg); err != nil {
		return nil, err
	}
	pins := map[string]string{}
	for _, section := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
		for name, version := range section {
			pins[name] = version
		}
	}
	return pins, nil
}

// pinChange is one package whose version setPins changed.
type pinChange struct{ Name, From, To string }

// setPins sets the version of each package of pins that the "dependencies" or "devDependencies" of
// the package.json in data lists, and changes nothing else: the other packages and keys stay, and
// so do the spaces, the line ends and the order. A package of pins that the file does not list is
// not added. A package that is already newer than its pin stays, since only the packages in strict
// must equal theirs, and a version that is not exact (a range, a tag, workspace:) is not guessed at:
// each of these gives a note that says so, and the file keeps it. The changes come sorted by name.
func setPins(data []byte, pins map[string]string, strict map[string]bool) ([]byte, []pinChange, []string, error) {
	type edit struct {
		start, end int
		change     pinChange
	}
	if !json.Valid(data) {
		return nil, nil, nil, errors.New("package.json is not valid JSON")
	}
	var edits []edit
	var notes []string
	fail := func(err error) ([]byte, []pinChange, []string, error) {
		return nil, nil, nil, fmt.Errorf("package.json: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, nil, errors.New("package.json is not a JSON object")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fail(err)
		}
		key, _ := keyTok.(string)
		if key != "dependencies" && key != "devDependencies" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return fail(err)
			}
			continue
		}
		if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
			return nil, nil, nil, fmt.Errorf("package.json: %q is not an object", key)
		}
		for dec.More() {
			nameTok, err := dec.Token()
			if err != nil {
				return fail(err)
			}
			name, _ := nameTok.(string)
			valTok, err := dec.Token()
			if err != nil {
				return fail(err)
			}
			from, ok := valTok.(string)
			if !ok {
				return nil, nil, nil, fmt.Errorf("package.json: the version of %s in %q is not a string", name, key)
			}
			to, pinned := pins[name]
			if !pinned || from == to {
				continue
			}
			// Only an exact version is compared. A range, a tag or workspace: says something
			// that is not guessed at.
			if semver.Canonical("v"+from) != "v"+from {
				if strict[name] {
					notes = append(notes, fmt.Sprintf("%s is written as %s, and Tifa needs exactly %s: set it by hand", name, from, to))
				} else {
					notes = append(notes, fmt.Sprintf("%s is written as %s, not an exact version, left as it is. Tifa is tested with %s", name, from, to))
				}
				continue
			}
			if !strict[name] && semver.Compare("v"+from, "v"+to) > 0 {
				notes = append(notes, fmt.Sprintf("%s %s is newer than the %s Tifa is tested with, left as it is", name, from, to))
				continue
			}
			// The offset after the value is the end of its quotes, so its start is a quote too.
			end := int(dec.InputOffset())
			start := end - len(from) - 2
			if start < 0 || data[start] != '"' || data[end-1] != '"' || string(data[start+1:end-1]) != from {
				return nil, nil, nil, fmt.Errorf("package.json: cannot find where the version of %s is written, is it written with escapes?", name)
			}
			edits = append(edits, edit{start + 1, end - 1, pinChange{name, from, to}})
		}
		if _, err := dec.Token(); err != nil {
			return fail(err)
		}
	}
	var out bytes.Buffer
	var changes []pinChange
	last := 0
	for _, e := range edits {
		out.Write(data[last:e.start])
		out.WriteString(e.change.To)
		last = e.end
		changes = append(changes, e.change)
	}
	out.Write(data[last:])
	sortPinChanges(changes)
	return out.Bytes(), changes, notes, nil
}

func sortPinChanges(c []pinChange) {
	slices.SortFunc(c, func(a, b pinChange) int { return strings.Compare(a.Name, b.Name) })
}

// upgradeNote is the text after one "Upgrading:" in the changelog entry of Version.
type upgradeNote struct{ Version, Text string }

var (
	changelogHeadRE = regexp.MustCompile(`^## \[([^\]]+)\]`)
	upgradingRE     = regexp.MustCompile(`Upgrading(?: from v?[0-9][0-9.]*)?:\s*`)
)

// upgradingNotes returns the "Upgrading:" notes of the entries of changelog for the versions after
// from, up to and including to, newest first as the changelog lists them. A note is what follows
// "Upgrading:" up to the end of its bullet, so an entry writes it last. Unreleased is no version,
// and text outside the bullets of an entry is not read.
func upgradingNotes(changelog, from, to string) []upgradeNote {
	var notes []upgradeNote
	version := ""
	var bullet []string
	flush := func() {
		if version == "" || len(bullet) == 0 {
			bullet = nil
			return
		}
		text := strings.Join(bullet, " ")
		bullet = nil
		if loc := upgradingRE.FindStringIndex(text); loc != nil {
			if note := strings.TrimSpace(text[loc[1]:]); note != "" {
				notes = append(notes, upgradeNote{version, note})
			}
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(changelog, "\r\n", "\n"), "\n") {
		if m := changelogHeadRE.FindStringSubmatch(line); m != nil {
			flush()
			version = ""
			if v := "v" + m[1]; semver.IsValid(v) && semver.Compare(v, from) > 0 && semver.Compare(v, to) <= 0 {
				version = v
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "#"), strings.TrimSpace(line) == "":
			flush()
			if strings.HasPrefix(line, "## ") {
				version = ""
			}
		case strings.HasPrefix(line, "- "):
			flush()
			bullet = []string{strings.TrimSpace(line[2:])}
		case bullet != nil:
			bullet = append(bullet, strings.TrimSpace(line))
		}
	}
	flush()
	return notes
}
