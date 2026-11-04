package bundle

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liustack/tifa/internal/content"
)

// Content is what an app with content collections adds to its build: the entries of
// dist/content.json, and the Markdown the tifa Vite plugin turns into the bodies in dist/content/.
type Content struct {
	File     content.File
	Markdown []content.Markdown
}

// markdownPackages are Vite and the packages of markdownPackages in js/vite/plugin.js, as pnpm
// add takes them.
const markdownPackages = "vite unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10"

// viteConfigs are the names Vite looks for its config under.
var viteConfigs = []string{"vite.config.ts", "vite.config.js", "vite.config.mjs", "vite.config.mts", "vite.config.cjs", "vite.config.cts"}

// prepareContent lists the entries for the tifa Vite plugin in .tifa/content/entries.json, before
// Vite runs. Without collections (c nil) it removes the list, so the plugin converts nothing and
// loads no package. An app without pages needs Vite only for entries, see convertAlone.
func prepareContent(root string, c *Content, pages bool) error {
	dir := filepath.Join(root, ".tifa", "content")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if c == nil || (!pages && len(c.Markdown) == 0) {
		return nil
	}
	if !pages {
		if err := checkViteFiles(root); err != nil {
			return err
		}
	}
	return writeEntries(root, c.Markdown)
}

// writeEntries lists entries in .tifa/content/entries.json for the tifa Vite plugin to convert,
// and removes the headings of an earlier conversion.
func writeEntries(root string, entries []content.Markdown) error {
	dir := filepath.Join(root, ".tifa", "content")
	if err := os.Remove(filepath.Join(root, ".tifa", filepath.FromSlash(content.HeadingsFile))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(content.Entries{Entries: entries})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".tifa", filepath.FromSlash(content.EntriesFile)), data, 0o644)
}

// checkViteFiles checks that an app without pages, whose entries the tifa plugin of Vite turns
// into HTML, has the package.json and the Vite config that takes, before Node starts.
func checkViteFiles(root string) error {
	var missing []string
	if _, err := os.Stat(filepath.Join(root, "package.json")); errors.Is(err, os.ErrNotExist) {
		missing = append(missing, "package.json")
	} else if err != nil {
		return err
	}
	config := false
	for _, name := range viteConfigs {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			config = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !config {
		missing = append(missing, "vite.config.ts")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("the app has content collections, and tifa turns their Markdown into HTML with Vite: add %s, which uses the tifa plugin:\n\n"+
		"\timport tifa from './.tifa/vite.js'\n\texport default { plugins: [tifa()] }\n\n"+
		"and install the packages:\n\n\tpnpm add -D %s", strings.Join(missing, " and "), markdownPackages)
}

// convertAlone turns the entries of an app without pages into HTML with the plugin of its Vite
// config, without bundles: the files of the bodies go into dist/assets. command names the tifa
// command for the messages.
func convertAlone(n *Node, c *Content, command string) error {
	if c == nil || len(c.Markdown) == 0 {
		return nil
	}
	return n.call("content", map[string]any{"alone": true, "command": command}, nil)
}

// finishContent writes dist/content.json once the plugin has written the bodies, with the
// headings it found in each. Without collections it removes the file.
func finishContent(root, dist string, c *Content) error {
	name := filepath.Join(dist, content.FileName)
	if c == nil {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return os.RemoveAll(filepath.Join(dist, "content"))
	}
	headings := map[string][]content.Heading{}
	if len(c.Markdown) > 0 {
		var err error
		if headings, err = readHeadings(root); err != nil {
			return err
		}
	}
	return writeContentFile(dist, c.File, headings)
}

// readHeadings reads the headings the tifa Vite plugin wrote for the entries it converted.
func readHeadings(root string) (map[string][]content.Heading, error) {
	data, err := os.ReadFile(filepath.Join(root, ".tifa", filepath.FromSlash(content.HeadingsFile)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("the tifa plugin of Vite wrote no %s: check that vite.config.ts uses it", content.HeadingsFile)
	}
	if err != nil {
		return nil, err
	}
	headings := map[string][]content.Heading{}
	if err := json.Unmarshal(data, &headings); err != nil {
		return nil, fmt.Errorf("%s: %w", content.HeadingsFile, err)
	}
	return headings, nil
}

// writeContentFile writes dist/content.json: the entries of f with the headings of each, by the
// file of its body.
func writeContentFile(dist string, f content.File, headings map[string][]content.Heading) error {
	if err := f.AddHeadings(headings); err != nil {
		return err
	}
	data, err := f.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dist, content.FileName), data, 0o644)
}
