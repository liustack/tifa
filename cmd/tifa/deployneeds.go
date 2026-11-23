package main

// deploy.auto.needs: the workflow that must pass before a deploy from GitHub Actions. The deploy
// workflow waits for it with the workflow_run event, which names the workflow by its name, and
// setup reads the file to find that name and to check that a push to the deploy branch runs it.
// Without that check a deploy would start with nothing to wait for, or never start.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// workflowsDir is where the workflow files of a repository are, relative to the root of the app.
const workflowsDir = ".github/workflows"

// resolveNeeds returns the name of the workflow file needs, which the deploy workflow waits for, or
// "" for needs none. It fails, with what to do, when the file is missing, has no name or does not
// run on a push to branch.
func resolveNeeds(root, needs, branch string) (string, error) {
	if needs == "none" {
		return "", nil
	}
	rel := workflowsDir + "/" + needs
	fix := " Fix the file, change deploy.auto.needs to another workflow file, or write needs: none to deploy on every push without waiting for a check."
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return "", errors.New(rel + " does not exist, and deploy.auto.needs says the deploy waits for it." + fix)
	}
	if err != nil {
		return "", err
	}
	var doc struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", errors.New(rel + " is not YAML." + fix)
	}
	// GitHub matches workflows in workflow_run by name. Its documentation names no other way, and
	// says only that a workflow without a name shows its file path, so a name is required.
	if strings.TrimSpace(doc.Name) == "" {
		return "", errors.New(rel + " has no name: line, and the deploy workflow finds the workflow it waits for by its name. Give it one, such as name: CI." + fix)
	}
	if ok, why := pushRunsOn(data, branch); !ok {
		return "", fmt.Errorf("%s does not run on a push to %s (%s), so a deploy would wait for a run that never comes.%s", rel, branch, why, fix)
	}
	return doc.Name, nil
}

// pushRunsOn reports whether the workflow in data runs on a push to branch, and if not, why. The
// on: key has three forms, a string, a list and a mapping of events (GitHub: workflow syntax, on).
// A paths filter is not looked at: it depends on the commit.
func pushRunsOn(data []byte, branch string) (bool, string) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return false, "it has no on: key"
	}
	var on *yaml.Node
	top := doc.Content[0]
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "on" {
			on = top.Content[i+1]
		}
	}
	if on == nil {
		return false, "it has no on: key"
	}
	switch on.Kind {
	case yaml.ScalarNode:
		if on.Value == "push" {
			return true, ""
		}
	case yaml.SequenceNode:
		for _, e := range on.Content {
			if e.Value == "push" {
				return true, ""
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(on.Content); i += 2 {
			if on.Content[i].Value == "push" {
				return pushFilter(on.Content[i+1], branch)
			}
		}
	}
	return false, "its on: has no push"
}

// pushFilter checks the filters of the push event for a push to branch.
func pushFilter(n *yaml.Node, branch string) (bool, string) {
	if n.Kind != yaml.MappingNode {
		return true, "" // push with no filter, or null
	}
	list := map[string][]string{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		v := n.Content[i+1]
		switch v.Kind {
		case yaml.ScalarNode:
			list[n.Content[i].Value] = []string{v.Value}
		case yaml.SequenceNode:
			for _, e := range v.Content {
				list[n.Content[i].Value] = append(list[n.Content[i].Value], e.Value)
			}
		}
	}
	branches, haveBranches := list["branches"]
	ignore, haveIgnore := list["branches-ignore"]
	_, haveTags := list["tags"]
	_, haveTagsIgnore := list["tags-ignore"]
	switch {
	case haveBranches && haveIgnore:
		return false, "GitHub does not allow both branches and branches-ignore for push"
	case haveBranches:
		if !branchFilterMatch(branches, branch) {
			return false, "its push branches are " + strings.Join(branches, ", ")
		}
	case haveIgnore:
		if branchFilterMatch(ignore, branch) {
			return false, "its push branches-ignore has " + branch
		}
	case haveTags || haveTagsIgnore:
		return false, "its push has a tags filter and no branches, which GitHub runs for tags only"
	}
	return true, ""
}

// branchFilterMatch applies the filter patterns of GitHub to a branch name, in order: a pattern that
// matches lets the branch in, and one that starts with ! and matches takes it out again. See
// "Filter pattern cheat sheet" in https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax
func branchFilterMatch(patterns []string, name string) bool {
	matched := false
	for _, p := range patterns {
		if rest, negate := strings.CutPrefix(p, "!"); negate {
			if filterRegexp(rest).MatchString(name) {
				matched = false
			}
		} else if filterRegexp(p).MatchString(name) {
			matched = true
		}
	}
	return matched
}

// filterRegexp turns a filter pattern into a regular expression for the whole name. * matches
// anything but /, ** anything, ? and + repeat the character before them (zero or one, one or
// more), and [] is a set of letters, digits and ranges of them. Any other character is itself.
func filterRegexp(pattern string) *regexp.Regexp {
	p := []rune(pattern) // a branch name may have any character, so the pattern is read by character
	var b strings.Builder
	b.WriteString("^")
	atoms := 0 // how many characters the pattern has put down so far, for ? and + to repeat
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '*':
			if i+1 < len(p) && p[i+1] == '*' {
				i++
				b.WriteString("(?:.*)")
			} else {
				b.WriteString("(?:[^/]*)")
			}
			atoms++
		case '?', '+':
			if atoms == 0 {
				b.WriteString(regexp.QuoteMeta(string(c)))
				atoms++
			} else {
				b.WriteRune(c)
			}
		case '[':
			end := slices.Index(p[i:], ']')
			if end < 0 || !setBody.MatchString(string(p[i+1:i+end])) {
				b.WriteString(`\[`)
			} else {
				b.WriteString("[" + string(p[i+1:i+end]) + "]")
				i += end
			}
			atoms++
		default:
			b.WriteString("(?:" + regexp.QuoteMeta(string(c)) + ")")
			atoms++
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile(`\A\z.`) // a pattern that cannot be read matches nothing
	}
	return re
}

// setBody is what GitHub allows inside []: letters, digits and ranges of them.
var setBody = regexp.MustCompile(`^(?:[a-zA-Z0-9](?:-[a-zA-Z0-9])?)+$`)
