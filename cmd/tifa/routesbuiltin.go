package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/symwalk"
)

// builtInRow is an address the program answers that is not a folder of app/, and when it does.
type builtInRow struct{ address, when string }

// builtInRows lists what the program answers besides the routes, for the table tifa routes prints
// after the Layouts. It reads the manifest, tifa.yaml and the public folder, and runs no code of
// the app. What only the running program knows, site.url set by an option or the environment
// and the tls_domains setting, stays a rule in the text. Rows that the app decides for good,
// such as the session and the pages, are there only when they hold.
func builtInRows(root string, m *analyze.Manifest) ([]builtInRow, error) {
	settings, err := codegen.BuildSettings(root)
	if err != nil {
		return nil, err
	}
	public := filepath.ToSlash(filepath.Clean(settings.Public))
	files, err := publicFiles(filepath.Join(root, settings.Public))
	if err != nil {
		return nil, err
	}

	site := "site.url is set"
	if m.SiteURL == "" {
		site += " (not in tifa.yaml)"
	}
	// A file of public/ answers first, then a route of the app, and the files of Tifa are left.
	// A route answers at its exact path only.
	taken := func(address string) string {
		if files[strings.TrimPrefix(address, "/")] {
			return public + address
		}
		for _, r := range m.Routes {
			if r.Path == address {
				return r.Dir
			}
		}
		return ""
	}
	siteFile := func(address, rule string) builtInRow {
		if by := taken(address); by != "" {
			return builtInRow{address, site + ", but " + by + " answers instead"}
		}
		return builtInRow{address, site + rule}
	}

	rows := []builtInRow{
		siteFile("/sitemap.xml", ""),
		{"/sitemap-<n>.xml", site + " and the sitemap has more than 50000 URLs or 50 MiB"},
		siteFile("/robots.txt", " and "+public+"/ has no robots.txt"),
		siteFile("/llms.txt", ""),
		siteFile("/llms-full.txt", ""),
	}
	md := site + " and tifa.SetSitemap lists a page with Content"
	if m.Content == nil || len(m.Content.Collections) == 0 {
		md += " (no content collections)"
	}
	rows = append(rows, builtInRow{"/<path>.md", md})
	if m.Session != nil {
		rows = append(rows, builtInRow{"/__tifa/session", "the app calls tifa.SetSession"})
	}
	for _, r := range m.Routes {
		if r.Page {
			rows = append(rows, builtInRow{"/assets/<file>", "the app has a page"})
			break
		}
	}
	if n := len(files); n > 0 {
		word := "files"
		if n == 1 {
			word = "file"
		}
		rows = append(rows, builtInRow{"/<file>", fmt.Sprintf("%s/ has %d %s", public, n, word)})
	}
	rows = append(rows, builtInRow{"/.well-known/acme-challenge/<token>", "server.tls_domains is set, on server.http_addr only"})
	return rows, nil
}

// publicFiles returns the files the program serves from the public folder, by their paths in it.
// They are the files tifa build copies: links are followed, and a name that starts with a dot is
// left out, apart from .well-known. A folder that is not there holds none.
func publicFiles(dir string) (map[string]bool, error) {
	files := map[string]bool{}
	err := symwalk.Walk(dir, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == dir {
			return fs.SkipAll
		}
		if err != nil {
			return err
		}
		if name := d.Name(); path != dir && strings.HasPrefix(name, ".") && name != ".well-known" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	return files, err
}
