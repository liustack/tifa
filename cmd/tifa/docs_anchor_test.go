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
	explicitID = regexp.MustCompile(`\s\{#([A-Za-z0-9_-]+)\}$`)
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
		text := strings.TrimSpace(line[hashes:])
		if m := explicitID.FindStringSubmatch(text); m != nil {
			ids[m[1]] = true
			continue
		}
		id := githubID(text)
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

// A heading that ends in {#id} has that id, which is how a Chinese doc keeps the ids of its English
// partner. The text of the heading does not make an id then.
func TestHeadingIDsReadTheExplicitID(t *testing.T) {
	ids := headingIDs("# 标题 {#title}\n\n## 文件夹 {#folders}\n\n## 没有写 id\n")
	for _, want := range []string{"title", "folders", "没有写-id"} {
		if !ids[want] {
			t.Errorf("no id %q in %v", want, ids)
		}
	}
	if ids["文件夹-folders"] || ids["标题-title"] {
		t.Errorf("the id text was taken from the heading: %v", ids)
	}
}

// Every .zh-CN.md doc has the headings of its English file, with the same ids.
func TestChineseDocsKeepTheIDsOfTheEnglishHeadings(t *testing.T) {
	root := filepath.Join("..", "..", "docs")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".zh-CN.md") {
			return err
		}
		zh, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		en, err := os.ReadFile(strings.TrimSuffix(path, ".zh-CN.md") + ".md")
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		zhIDs, enIDs := headingIDs(string(zh)), headingIDs(string(en))
		for id := range enIDs {
			if !zhIDs[id] {
				t.Errorf("%s has no heading with the id %s of its English file", path, id)
			}
		}
		for id := range zhIDs {
			if !enIDs[id] {
				t.Errorf("%s has a heading with the id %s, which its English file has not", path, id)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
