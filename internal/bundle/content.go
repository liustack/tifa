package bundle

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/liustack/tifa/internal/content"
)

// Content is what an app with content collections adds to its build: the entries of
// dist/content.json, and the Markdown the tifa Vite plugin turns into the bodies in dist/content/.
type Content struct {
	File     content.File
	Markdown []content.Markdown
}

// errContentWithoutPages is the error of an app whose collections have entries and which has no
// page, so no Vite runs to turn them into HTML.
var errContentWithoutPages = errors.New("the app has content collections, whose Markdown tifa turns into HTML with the tifa plugin of vite.config.ts, and no page.tsx, so Vite does not run: add a page to the app")

// prepareContent lists the entries for the tifa Vite plugin in .tifa/content/entries.json, before
// Vite runs. Without collections (c nil) it removes the list, so the plugin converts nothing and
// loads no package.
func prepareContent(root string, c *Content, pages bool) error {
	dir := filepath.Join(root, ".tifa", "content")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if c == nil {
		return nil
	}
	if !pages && len(c.Markdown) > 0 {
		return errContentWithoutPages
	}
	if !pages {
		return nil
	}
	data, err := json.Marshal(content.Entries{Entries: c.Markdown})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".tifa", filepath.FromSlash(content.EntriesFile)), data, 0o644)
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
		data, err := os.ReadFile(filepath.Join(root, ".tifa", filepath.FromSlash(content.HeadingsFile)))
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("the tifa plugin of Vite wrote no %s: check that vite.config.ts uses it", content.HeadingsFile)
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &headings); err != nil {
			return fmt.Errorf("%s: %w", content.HeadingsFile, err)
		}
	}
	if err := c.File.AddHeadings(headings); err != nil {
		return err
	}
	data, err := c.File.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}
