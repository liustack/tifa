package foreign

import (
	"bufio"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// publicExts are the files of public/ that a page uses and that an import would hash and cache for
// a year: images and fonts.
var publicExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".svg": true, ".ico": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
}

var (
	// quotedPath is a string literal whose whole value is a path from the root of the site.
	quotedPath = regexp.MustCompile("[\"'`](/[^/\"'`\\s()][^\"'`\\s()]*)[\"'`]")
	// cssURL is a url() with a path from the root of the site.
	cssURL = regexp.MustCompile(`url\(\s*["']?(/[^/"'\s()][^"'\s()]*)["']?\s*\)`)
)

// PublicImports finds the images and fonts of the public folder that the source refers to by their
// path, where an import would do: a string literal or a CSS url() in a file under app/ whose value
// is /<path>, with the file in the public folder (public is relative to root, as build.public).
// Files that browsers and other sites ask for by name are left out: favicon.ico, the apple touch
// icons and what a web manifest lists. Every finding is a warning. It reads files only.
func PublicImports(root, public string) ([]Finding, error) {
	pubDir := filepath.Join(root, filepath.FromSlash(public))
	if info, err := os.Stat(pubDir); err != nil || !info.IsDir() {
		return nil, nil
	}
	files, err := SourceFiles(root)
	if err != nil {
		return nil, err
	}
	css, err := cssFiles(root)
	if err != nil {
		return nil, err
	}
	files = append(files, css...)
	manifests := manifestText(pubDir)
	public = strings.Trim(filepath.ToSlash(public), "/")
	var out []Finding
	for _, file := range files {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}
		rel = filepath.ToSlash(rel)
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for line := 1; sc.Scan(); line++ {
			text := sc.Text()
			seen := map[string]bool{}
			for _, re := range []*regexp.Regexp{quotedPath, cssURL} {
				for _, m := range re.FindAllStringSubmatch(text, -1) {
					p := m[1]
					if i := strings.IndexAny(p, "?#"); i >= 0 {
						p = p[:i]
					}
					if seen[p] || !importable(pubDir, p, manifests) {
						continue
					}
					seen[p] = true
					out = append(out, finding(rel, line, strings.Index(text, m[0])+1, publicRule(p, public, strings.HasSuffix(rel, ".css") || re == cssURL)))
				}
			}
		}
		err = sc.Err()
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	return sortFindings(out), nil
}

// importable says whether p, a path from the root of the site, names an image or a font in the
// public folder that nothing asks for by name.
func importable(pubDir, p, manifests string) bool {
	if !publicExts[strings.ToLower(path.Ext(p))] || strings.HasPrefix(p, "/assets/") {
		return false
	}
	base := strings.ToLower(path.Base(p))
	if base == "favicon.ico" || strings.HasPrefix(base, "apple-touch-icon") && strings.HasSuffix(base, ".png") {
		return false
	}
	name := p[1:]
	if !fs.ValidPath(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(pubDir, filepath.FromSlash(name)))
	if err != nil || info.IsDir() {
		return false
	}
	return !strings.Contains(manifests, `"`+p+`"`) && !strings.Contains(manifests, `"`+name+`"`)
}

// manifestText is the text of the web manifests in the public folder, which name icons by path.
func manifestText(pubDir string) string {
	var b strings.Builder
	for _, name := range []string{"manifest.webmanifest", "site.webmanifest"} {
		if data, err := os.ReadFile(filepath.Join(pubDir, name)); err == nil {
			b.Write(data)
		}
	}
	return b.String()
}

// cssFiles lists the stylesheets under app/.
func cssFiles(root string) ([]string, error) {
	appDir := filepath.Join(root, "app")
	if info, err := os.Stat(appDir); err != nil || !info.IsDir() {
		return nil, nil
	}
	var files []string
	err := filepath.WalkDir(appDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) == ".css" {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

func publicRule(p, public string, css bool) Rule {
	base := path.Base(p)
	example := "import " + importName(base) + " from './" + base + "' and src={" + importName(base) + "}"
	if css {
		example = "url(./" + base + ")"
	}
	return Rule{
		Says: p + " is a file of " + public + "/, which is served with no-cache and a name without a hash",
		Instead: "move it next to the code that uses it and import it, so that it is hashed and cached for a year: " + example +
			". Keep " + public + "/ for files that must keep their name",
		Doc:     docPublic,
		Warning: true,
	}
}

// importName is a JavaScript name for the file base, such as logo for logo.png.
func importName(base string) string {
	name := strings.TrimSuffix(base, path.Ext(base))
	var b strings.Builder
	upper := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9' && b.Len() > 0:
			if upper {
				r = []rune(strings.ToUpper(string(r)))[0]
				upper = false
			}
			b.WriteRune(r)
		default:
			upper = b.Len() > 0
		}
	}
	if b.Len() == 0 {
		return "asset"
	}
	return b.String()
}
