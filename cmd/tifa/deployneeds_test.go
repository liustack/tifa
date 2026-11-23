package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The filter patterns of GitHub for branches: * stops at a slash, ** does not, ? and + repeat the
// character before them, [] holds a set, and ! at the start takes away what the patterns before it
// let in.
func TestBranchFilterPatterns(t *testing.T) {
	for name, tc := range map[string]struct {
		patterns []string
		branch   string
		want     bool
	}{
		"plain":                 {[]string{"main"}, "main", true},
		"plain other":           {[]string{"main"}, "master", false},
		"star":                  {[]string{"release-*"}, "release-1", true},
		"star stops at slash":   {[]string{"release-*"}, "release-1/x", false},
		"double star":           {[]string{"release/**"}, "release/1/x", true},
		"double star alone":     {[]string{"**"}, "a/b/c", true},
		"question zero":         {[]string{"v1?"}, "v", true},
		"question zero of x":    {[]string{"colou?r"}, "color", true},
		"question one of x":     {[]string{"colou?r"}, "colour", true},
		"plus one":              {[]string{"a+b"}, "aab", true},
		"plus none":             {[]string{"a+b"}, "b", false},
		"set":                   {[]string{"[CB]at"}, "Bat", true},
		"set miss":              {[]string{"[CB]at"}, "Rat", false},
		"range":                 {[]string{"v[1-2]00"}, "v200", true},
		"range miss":            {[]string{"v[1-2]00"}, "v300", false},
		"negate":                {[]string{"release/**", "!release/old"}, "release/old", false},
		"negate keeps others":   {[]string{"release/**", "!release/old"}, "release/new", true},
		"negate before":         {[]string{"!release/old", "release/**"}, "release/old", true},
		"utf8 star":             {[]string{"发布/**"}, "发布/正式", true},
		"utf8 single":           {[]string{"发布?"}, "发", true},
		"utf8 question":         {[]string{"发布?"}, "发布", true},
		"utf8 literal miss":     {[]string{"发布/*"}, "发/布", false},
		"dot is a dot":          {[]string{"v1.0"}, "v1x0", false},
		"must match the whole":  {[]string{"main"}, "main2", false},
		"bang in the middle":    {[]string{"a!b"}, "a!b", true},
		"regexp characters are": {[]string{"(a|b)"}, "a", false},
	} {
		if got := branchFilterMatch(tc.patterns, tc.branch); got != tc.want {
			t.Errorf("%s: %v on %q: %v", name, tc.patterns, tc.branch, got)
		}
	}
}

func TestPushRunsOn(t *testing.T) {
	for name, tc := range map[string]struct {
		on     string
		branch string
		want   bool
	}{
		"string":                   {"on: push\n", "main", true},
		"string other":             {"on: pull_request\n", "main", false},
		"list":                     {"on: [pull_request, push]\n", "main", true},
		"list without":             {"on: [pull_request, workflow_dispatch]\n", "main", false},
		"block list":               {"on:\n  - push\n", "main", true},
		"map null":                 {"on:\n  pull_request:\n  push:\n", "main", true},
		"map empty":                {"on:\n  push: {}\n", "main", true},
		"map without push":         {"on:\n  pull_request:\n    branches: [main]\n", "main", false},
		"branches":                 {"on:\n  push:\n    branches: [main]\n", "main", true},
		"branches miss":            {"on:\n  push:\n    branches: [develop]\n", "main", false},
		"branches one word":        {"on:\n  push:\n    branches: main\n", "main", true},
		"branches glob":            {"on:\n  push:\n    branches: ['release/**']\n", "release/2", true},
		"branches ignore":          {"on:\n  push:\n    branches-ignore: [main]\n", "main", false},
		"branches ignore other":    {"on:\n  push:\n    branches-ignore: [wip/**]\n", "main", true},
		"tags only":                {"on:\n  push:\n    tags: ['v*']\n", "main", false},
		"tags ignore only":         {"on:\n  push:\n    tags-ignore: ['v*']\n", "main", false},
		"tags and branches":        {"on:\n  push:\n    tags: ['v*']\n    branches: [main]\n", "main", true},
		"paths do not decide":      {"on:\n  push:\n    paths: ['**.go']\n", "main", true},
		"pull request only branch": {"on:\n  pull_request:\n    branches: [main]\n  push:\n    branches: [develop]\n", "main", false},
	} {
		got, why := pushRunsOn([]byte(tc.on+"jobs: {}\n"), tc.branch)
		if got != tc.want || (!got && why == "") {
			t.Errorf("%s: %v (%q)", name, got, why)
		}
	}
	if ok, why := pushRunsOn([]byte("on:\n  push:\n    branches: [a]\n    branches-ignore: [b]\n"), "a"); ok || !strings.Contains(why, "branches-ignore") {
		t.Errorf("both filters: %v %q", ok, why)
	}
	if ok, why := pushRunsOn([]byte("name: CI\n"), "main"); ok || !strings.Contains(why, "on:") {
		t.Errorf("no on: %v %q", ok, why)
	}
}

func TestResolveNeeds(t *testing.T) {
	write := func(t *testing.T, name, text string) string {
		root := t.TempDir()
		if text != "" {
			writeFile(t, filepath.Join(root, ".github", "workflows", name), text)
		}
		return root
	}
	t.Run("none waits for nothing", func(t *testing.T) {
		if name, err := resolveNeeds(t.TempDir(), "none", "main"); name != "" || err != nil {
			t.Errorf("%q, %v", name, err)
		}
	})
	t.Run("the name of the file", func(t *testing.T) {
		root := write(t, "checks.yaml", "name: Checks\non: push\n")
		if name, err := resolveNeeds(root, "checks.yaml", "main"); name != "Checks" || err != nil {
			t.Errorf("%q, %v", name, err)
		}
	})
	fix := "Fix the file, change deploy.auto.needs to another workflow file, or write needs: none"
	for name, tc := range map[string]struct{ file, text, want string }{
		"missing":      {"", "", ".github/workflows/ci.yml does not exist"},
		"no name":      {"ci.yml", "on: push\n", "has no name: line"},
		"empty name":   {"ci.yml", "name: \"\"\non: push\n", "has no name: line"},
		"not yaml":     {"ci.yml", "name: [\n", ".github/workflows/ci.yml is not YAML"},
		"not on push":  {"ci.yml", "name: CI\non: pull_request\n", "does not run on a push to main"},
		"other branch": {"ci.yml", "name: CI\non:\n  push:\n    branches: [develop]\n", "does not run on a push to main"},
	} {
		root := write(t, tc.file, tc.text)
		_, err := resolveNeeds(root, "ci.yml", "main")
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), fix) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
