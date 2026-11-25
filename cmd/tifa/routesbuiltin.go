package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/codegen"
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
	scan, err := bundle.ScanPublic(filepath.Join(root, settings.Public), settings.PublicDotfiles)
	if err != nil {
		return nil, err
	}
	files := scan.Files

	site := "site.url is set"
	if m.SiteURL == "" {
		site += " (not in tifa.yaml)"
	}
	// A file of public/ answers first, then a route of the app, and the files of Tifa are left.
	// A route answers at its exact path only.
	taken := func(address string) string {
		if files[strings.TrimPrefix(address, "/")] != nil {
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
	rows = append(rows, publicRow(public, len(files), scan.LeftOut)...)
	rows = append(rows, builtInRow{"/.well-known/acme-challenge/<token>", "server.tls_domains is set, on server.http_addr only"})
	return rows, nil
}

// publicRow is the row of the files of the public folder: how many there are, and how many dot
// files are left out. There is none when the folder holds nothing at all.
func publicRow(public string, n int, left []string) []builtInRow {
	if n == 0 && len(left) == 0 {
		return nil
	}
	count := "no files"
	switch n {
	case 0:
	case 1:
		count = "1 file"
	default:
		count = fmt.Sprintf("%d files", n)
	}
	when := fmt.Sprintf("%s/ has %s", public, count)
	if len(left) > 0 {
		when += fmt.Sprintf(" (%s left out)", dotNames(left))
	}
	return []builtInRow{{"/<file>", when}}
}
