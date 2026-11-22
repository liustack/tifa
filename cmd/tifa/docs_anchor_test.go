package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// Every link of a doc to a heading, in the same doc or another one, names a heading that exists.
// A heading that is renamed leaves its links pointing nowhere, and the site of Tifa and its
// translations keep these ids.
func TestDocLinksToHeadingsFindThem(t *testing.T) {
	root := filepath.Join("..", "..")
	files := map[string]string{} // clean path -> text
	for _, dir := range []string{"docs"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[filepath.Clean(path)] = string(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"README.md", "README.zh-CN.md", "CHANGELOG.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.Clean(filepath.Join(root, name))] = string(data)
	}
	ids := map[string]map[string]bool{}
	for path, text := range files {
		ids[path] = headingIDs(text)
	}
	for path, text := range files {
		for _, target := range docLinks(text) {
			file, anchor, ok := strings.Cut(target, "#")
			if !ok || anchor == "" || strings.Contains(file, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			to := path
			if file != "" {
				if filepath.Ext(file) != ".md" {
					continue
				}
				to = filepath.Clean(filepath.Join(filepath.Dir(path), file))
			}
			known, ok := ids[to]
			if !ok {
				continue // a file outside the checked ones
			}
			if !known[anchor] {
				t.Errorf("%s: the link %s names no heading of %s", path, target, to)
			}
		}
	}
}

var (
	linkTarget = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	htmlID     = regexp.MustCompile(`\sid="([^"]+)"`)
)

// docLinks returns the targets of the Markdown links of a text, outside code.
func docLinks(text string) []string {
	var out []string
	for _, line := range proseLines(text) {
		for _, m := range linkTarget.FindAllStringSubmatch(withoutInlineCode(line), -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// headingIDs returns the ids of the headings of a text, made the way GitHub makes them, and the
// ids written in its HTML.
func headingIDs(text string) map[string]bool {
	ids := map[string]bool{}
	seen := map[string]int{}
	for _, line := range proseLines(text) {
		for _, m := range htmlID.FindAllStringSubmatch(line, -1) {
			ids[m[1]] = true
		}
		hashes := len(line) - len(strings.TrimLeft(line, "#"))
		if hashes == 0 || hashes > 6 || !strings.HasPrefix(line[hashes:], " ") {
			continue
		}
		id := githubID(strings.TrimSpace(line[hashes:]))
		if n := seen[id]; n > 0 {
			seen[id] = n + 1
			id += "-" + strconv.Itoa(n)
		} else {
			seen[id] = 1
		}
		ids[id] = true
	}
	return ids
}

// githubID is the id GitHub gives a heading: lower case, without punctuation, spaces as -.
func githubID(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// proseLines returns the lines of a text outside fenced code and the front matter.
func proseLines(text string) []string {
	if strings.HasPrefix(text, "---\n") {
		if _, rest, ok := strings.Cut(text[len("---\n"):], "\n---\n"); ok {
			text = rest
		}
	}
	var out []string
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if fence == "" && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
			fence = trimmed[:3]
			continue
		}
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		out = append(out, line)
	}
	return out
}

// withoutInlineCode drops the `code` spans of a line.
func withoutInlineCode(line string) string {
	parts := strings.Split(line, "`")
	var b strings.Builder
	for i, p := range parts {
		if i%2 == 0 {
			b.WriteString(p)
		}
	}
	return b.String()
}
