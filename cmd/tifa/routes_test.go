package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// builtIn returns the BUILT IN table of tifa routes, from its header on.
func builtIn(t *testing.T, args ...string) string {
	t.Helper()
	out, stderr, err := run(t, append([]string{"routes"}, args...)...)
	if err != nil {
		t.Fatalf("routes: %v\n%s%s", err, out, stderr)
	}
	_, table, ok := strings.Cut(out, "BUILT IN")
	if !ok {
		t.Fatalf("no BUILT IN table in:\n%s", out)
	}
	return "BUILT IN" + table
}

// row returns the line of the table that starts with the address, its fields joined by one space.
func row(table, address string) string {
	for line := range strings.SplitSeq(table, "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == address {
			return strings.Join(f, " ")
		}
	}
	return ""
}

// wantRow fails the test unless the row of address is the address followed by when.
func wantRow(t *testing.T, table, address, when string) {
	t.Helper()
	if got := row(table, address); got != address+" "+when {
		t.Errorf("row of %s is\n  %q\nwant\n  %q\nin:\n%s", address, got, address+" "+when, table)
	}
}

func removePublicRobots(t *testing.T, dir string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, "public", "robots.txt")); err != nil {
		t.Fatal(err)
	}
}

const siteYAML = "site:\n  url: https://shop.example.com\n"

// With site.url in tifa.yaml the files of a site are listed as they are: nothing says they may
// be missing.
func TestRoutesBuiltInWithSiteURL(t *testing.T) {
	dir := apiApp(t)
	writeFile(t, filepath.Join(dir, "tifa.yaml"), siteYAML)
	removePublicRobots(t, dir)
	table := builtIn(t, "--dir", dir)
	if got := strings.Fields(strings.SplitN(table, "\n", 2)[0]); len(got) != 3 || got[0] != "BUILT" || got[1] != "IN" || got[2] != "WHEN" {
		t.Errorf("header is %q", got)
	}
	wantRow(t, table, "/sitemap.xml", "site.url is set")
	wantRow(t, table, "/sitemap-<n>.xml", "site.url is set and the sitemap has more than 50000 URLs or 50 MiB")
	wantRow(t, table, "/robots.txt", "site.url is set and public/ has no robots.txt")
	wantRow(t, table, "/llms.txt", "site.url is set")
	wantRow(t, table, "/llms-full.txt", "site.url is set")
	wantRow(t, table, "/<path>.md", "site.url is set and tifa.SetSitemap lists a page with Content (no content collections)")
}

// site.url can be set when the program runs, so the rows stay without it in tifa.yaml and say that
// it is not there.
func TestRoutesBuiltInWithoutSiteURL(t *testing.T) {
	dir := apiApp(t)
	removePublicRobots(t, dir)
	table := builtIn(t, "--dir", dir)
	wantRow(t, table, "/sitemap.xml", "site.url is set (not in tifa.yaml)")
	wantRow(t, table, "/robots.txt", "site.url is set (not in tifa.yaml) and public/ has no robots.txt")
	wantRow(t, table, "/llms.txt", "site.url is set (not in tifa.yaml)")
	wantRow(t, table, "/llms-full.txt", "site.url is set (not in tifa.yaml)")
}

// A robots.txt of public/ answers before the one of Tifa does.
func TestRoutesBuiltInPublicRobots(t *testing.T) {
	dir := apiApp(t) // has public/robots.txt
	writeFile(t, filepath.Join(dir, "tifa.yaml"), siteYAML)
	table := builtIn(t, "--dir", dir)
	wantRow(t, table, "/robots.txt", "site.url is set, but public/robots.txt answers instead")
	wantRow(t, table, "/sitemap.xml", "site.url is set")
	wantRow(t, table, "/<file>", "public/ has 1 file")
}

// A route of the app at the same path answers before Tifa does.
func TestRoutesBuiltInOwnRoute(t *testing.T) {
	dir := apiApp(t)
	removePublicRobots(t, dir)
	writeFile(t, filepath.Join(dir, "tifa.yaml"), siteYAML)
	writeFile(t, filepath.Join(dir, "app", "robots.txt", "route.go"), `package robots

import "github.com/liustack/tifa"

func GET(c *tifa.Ctx) error { return c.JSON(200, "mine") }
`)
	table := builtIn(t, "--dir", dir)
	wantRow(t, table, "/robots.txt", "site.url is set, but app/robots.txt answers instead")
	wantRow(t, table, "/sitemap.xml", "site.url is set")
	if strings.Contains(table, "/<file>") {
		t.Errorf("public/ has no file left:\n%s", table)
	}
}

// The session path and the files of the browser bundle depend on what the app has.
func TestRoutesBuiltInSessionAndAssets(t *testing.T) {
	wantRow(t, builtIn(t, "--dir", testApp("sessionok")), "/__tifa/session", "the app calls tifa.SetSession")
	without := builtIn(t, "--dir", testApp("valid"))
	if row(without, "/__tifa/session") != "" {
		t.Errorf("session row for an app without one:\n%s", without)
	}
	wantRow(t, without, "/assets/<file>", "the app has a page")
	if got := builtIn(t, "--dir", apiApp(t)); row(got, "/assets/<file>") != "" {
		t.Errorf("assets row for an app without pages:\n%s", got)
	}
}

// The Markdown versions need a page that shows a content entry.
func TestRoutesBuiltInMarkdown(t *testing.T) {
	wantRow(t, builtIn(t, "--dir", testApp("content")), "/<path>.md", "site.url is set (not in tifa.yaml) and tifa.SetSitemap lists a page with Content")
}

// Certificates are asked for on the plain HTTP address, whatever the app holds.
func TestRoutesBuiltInChallenge(t *testing.T) {
	wantRow(t, builtIn(t, "--dir", testApp("valid")), "/.well-known/acme-challenge/<token>", "server.tls_domains is set, on server.http_addr only")
}

// The /<file> row says how many dot files of public/ are left out, and counts the files that are
// served without them. build.public_dotfiles puts a named one back.
func TestRoutesBuiltInPublicDotFiles(t *testing.T) {
	dir := apiApp(t) // has public/robots.txt
	writeFile(t, filepath.Join(dir, "public", ".DS_Store"), "x")
	writeFile(t, filepath.Join(dir, "public", ".foo", "a.txt"), "x")
	writeFile(t, filepath.Join(dir, "public", ".nojekyll"), "")
	wantRow(t, builtIn(t, "--dir", dir), "/<file>", "public/ has 1 file (3 dot files or folders left out)")

	writeFile(t, filepath.Join(dir, "tifa.yaml"), "build:\n  public_dotfiles: .nojekyll\n")
	wantRow(t, builtIn(t, "--dir", dir), "/<file>", "public/ has 2 files (2 dot files or folders left out)")
}

// A public folder with nothing but dot files still gets the row, which says why it is empty.
func TestRoutesBuiltInPublicHoldsOnlyDotFiles(t *testing.T) {
	dir := apiApp(t)
	removePublicRobots(t, dir)
	writeFile(t, filepath.Join(dir, "public", ".DS_Store"), "x")
	wantRow(t, builtIn(t, "--dir", dir), "/<file>", "public/ has no files (1 dot file left out)")
}

// A name that build.public_dotfiles lists and public/ lacks stops tifa routes like the other commands.
func TestRoutesFailOnADotFileThatIsNotThere(t *testing.T) {
	dir := apiApp(t)
	writeFile(t, filepath.Join(dir, "tifa.yaml"), "build:\n  public_dotfiles: .nojekyll\n")
	_, _, err := run(t, "routes", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), `build.public_dotfiles: ".nojekyll" is not in public/`) {
		t.Errorf("routes: %v", err)
	}
}
