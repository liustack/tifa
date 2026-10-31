package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

func newDocsCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Print where the docs of the tifa version this app uses are, and what each one covers",
		Long: `docs finds the tifa module the application's go.mod requires (go list -m, so a replace
directive works too) and prints the folder that holds its docs, then the same index as
scripts/docs-list.sh in the tifa repository: the summary of each doc and when to read it. The docs
are those of the tifa version the application builds with, not those of the main branch.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			mod, err := findTifaModule(cmd.Context(), root)
			if err != nil {
				return err
			}
			index, warnings, err := docsIndex(mod.Dir)
			if err != nil {
				return err
			}
			for _, w := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s %s", tifaModule, mod.Version)
			if mod.Replace != nil {
				fmt.Fprintf(out, " (replaced by %s)", mod.Replace.Path)
			}
			fmt.Fprintf(out, "\nDocs: %s\nThe paths below are relative to %s.\n\n%s\n", filepath.Join(mod.Dir, "docs"), mod.Dir, index)
			fmt.Fprintln(out, `Read the docs whose "Read when" applies before you change code.`)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	return cmd
}

// goModule is what go list -m -json prints about a module, as far as tifa docs reads it.
type goModule struct {
	Version string
	Dir     string
	Replace *struct{ Path string }
}

// findTifaModule asks the go command for the tifa module of the application at root.
func findTifaModule(ctx context.Context, root string) (goModule, error) {
	list := exec.CommandContext(ctx, "go", "list", "-m", "-json", tifaModule)
	list.Dir = root
	var stderr bytes.Buffer
	list.Stderr = &stderr
	out, err := list.Output()
	if err != nil {
		return goModule{}, fmt.Errorf("go list -m %s in %s: %w\n%s", tifaModule, root, err, strings.TrimSpace(stderr.String()))
	}
	return parseModule(out)
}

// parseModule reads the JSON of go list -m. A module without a Dir is not in the module cache yet.
func parseModule(data []byte) (goModule, error) {
	var mod goModule
	if err := json.Unmarshal(data, &mod); err != nil {
		return goModule{}, fmt.Errorf("go list -m -json: %w", err)
	}
	if mod.Dir == "" {
		return goModule{}, fmt.Errorf("the files of %s %s are not downloaded, run: go mod download %s", tifaModule, mod.Version, tifaModule)
	}
	return mod, nil
}

// docsIndex lists the docs of the tifa module in dir like scripts/docs-list.sh: for each
// Markdown file under docs/ but the Chinese versions (*.zh-CN.md), docs/archive and docs/research,
// in byte order of the paths, a line with the path and the summary of its front matter, and a line
// with when to read it. A doc without a summary is left out, and a warning says so.
func docsIndex(dir string) (index string, warnings []string, err error) {
	var paths []string
	err = fs.WalkDir(os.DirFS(dir), "docs", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if p == "docs/archive" || p == "docs/research" {
				return fs.SkipDir
			}
		case strings.HasSuffix(p, ".md") && !strings.HasSuffix(p, ".zh-CN.md"):
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	slices.Sort(paths)
	var out strings.Builder
	for _, p := range paths {
		text, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return "", nil, err
		}
		summary, when, ok := frontMatter(string(text))
		if !ok {
			warnings = append(warnings, p+" has no front matter summary")
			continue
		}
		fmt.Fprintf(&out, "%s - %s\n", p, summary)
		if len(when) > 0 {
			fmt.Fprintf(&out, "  Read when: %s\n", strings.Join(when, " | "))
		}
	}
	return out.String(), warnings, nil
}

// frontMatter reads the summary and read_when of the front matter that starts text, in the two
// forms scripts/docs-list.sh reads: a list of "- item" lines, or [a, b] on the key's line. ok is
// false when there is no front matter or no summary.
func frontMatter(text string) (summary string, when []string, ok bool) {
	lines := strings.Split(text, "\n")
	if lines[0] != "---" {
		return "", nil, false
	}
	closed, inList := false, false
	for _, line := range lines[1:] {
		if line == "---" {
			closed = true
			break
		}
		switch {
		case strings.HasPrefix(line, "summary:"):
			summary = unquote(strings.TrimPrefix(line, "summary:"))
			inList = false
		case strings.HasPrefix(line, "read_when:"):
			rest := unquote(strings.TrimPrefix(line, "read_when:"))
			inList = true
			if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
				inList = false
				for item := range strings.SplitSeq(rest[1:len(rest)-1], ",") {
					if item = unquote(item); item != "" {
						when = append(when, item)
					}
				}
			}
		case inList && isListItem(line):
			when = append(when, unquote(strings.TrimLeft(strings.TrimLeft(line, " \t")[1:], " \t")))
		case line != "" && line[0] != ' ' && line[0] != '\t':
			inList = false
		}
	}
	return summary, when, closed && summary != ""
}

// isListItem reports whether line is an indented "- item" line.
func isListItem(line string) bool {
	rest := strings.TrimLeft(line, " \t")
	return len(rest) < len(line) && strings.HasPrefix(rest, "-") && len(rest) > 1 && (rest[1] == ' ' || rest[1] == '\t')
}

// unquote trims the spaces around s and one pair of single or double quotes.
func unquote(s string) string {
	s = strings.Trim(s, " \t")
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
