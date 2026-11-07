package analyze

import (
	"errors"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/langs"
)

// Area is a folder of app/ with a _locales folder: the routes in it and below it have the
// languages of its files, until a folder below holds another area.
type Area struct {
	// Dir is the folder relative to the application root, such as "app/store".
	Dir string `json:"dir"`
	// Languages are the languages of the area, the default language first and then the others
	// in byte order.
	Languages []string `json:"languages"`
}

// I18n is what the _locales folders and the i18n and site settings of tifa.yaml say about the
// languages of the app. It is nil for an app without a _locales folder.
type I18n struct {
	// Default is the default language: i18n.default, or the only language of the app.
	Default string `json:"default"`
	// Always is i18n.prefix: always, every route of an area has the language in its URL, the
	// default language too.
	Always bool `json:"always"`
	// Languages are the languages of the site, the union of the areas, the default language first
	// and then the others in byte order.
	Languages []string `json:"languages"`
	// SiteURL is site.url, empty when not set.
	SiteURL string `json:"siteUrl,omitempty"`
	// Areas is sorted by Dir.
	Areas []Area `json:"areas"`
}

// readLocales reads the file names of the _locales folder at dir, a folder of app/ with the ID id
// (its own ID, ending in /_locales). <lang>.draft.json and files that are not .json are not
// languages. A name that is not a language tag is reported.
func (a *analyzer) readLocales(dir, id string) error {
	found, invalid, err := langs.Read(dir)
	if err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(invalid)) {
		a.errs = append(a.errs, Error{Pos: token.Position{Filename: id + "/" + name}, Msg: invalid[name].Error()})
	}
	if a.locales == nil {
		a.locales = map[string][]string{}
	}
	a.locales[areaOf(id)] = found
	return nil
}

// areaOf returns the folder part of a _locales folder ID: app/store/_locales is the area app/store.
func areaOf(id string) string { return strings.TrimSuffix(id, "/"+langs.Dir) }

// i18n works out the areas, the languages of the site and the language of each route and Layout
// from the _locales folders and tifa.yaml, and reports what does not go together. It returns nil
// for an app without a _locales folder.
func (a *analyzer) i18n(routes []*route, layouts []*layout) *I18n {
	if len(a.locales) == 0 {
		return nil
	}
	cfg := config.Default()
	data, err := os.ReadFile(filepath.Join(a.root, "tifa.yaml"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		a.errs = append(a.errs, Error{Pos: token.Position{Filename: "tifa.yaml"}, Msg: err.Error()})
		return nil
	}
	if cfg, _, err = config.Project(data); err != nil {
		// codegen reports the same, but an app with languages must not be read with a guessed default.
		a.errs = append(a.errs, Error{Pos: token.Position{Filename: "tifa.yaml"}, Msg: err.Error()})
		return nil
	}
	site := map[string]bool{}
	for dir, ls := range a.locales {
		if len(ls) == 0 {
			a.errs = append(a.errs, Error{Pos: token.Position{Filename: dir + "/" + langs.Dir}, Msg: "the area has no language: add a file named after a language tag, such as zh-CN.json"})
		}
		for _, l := range ls {
			site[l] = true
		}
	}
	var all []string
	for l := range site {
		all = append(all, l)
	}
	slices.Sort(all)
	def := cfg.I18n.Default
	switch {
	case def == "" && len(all) == 1:
		def = all[0]
	case def == "" && len(all) > 1:
		a.errs = append(a.errs, Error{Pos: token.Position{Filename: "tifa.yaml"}, Msg: "the app has several languages (" + strings.Join(all, ", ") + "), so i18n.default must name the default one"})
	case def != "" && len(all) > 0 && !site[def]:
		a.errs = append(a.errs, Error{Pos: token.Position{Filename: "tifa.yaml"}, Msg: "i18n.default is \"" + def + "\", which no _locales folder has a file for (" + strings.Join(all, ", ") + ")"})
	}
	m := &I18n{Default: def, Always: cfg.I18n.Prefix == "always", Languages: langs.Order(all, def), SiteURL: cfg.Site.URL}
	if len(all) > 1 && cfg.Site.URL == "" {
		a.errs = append(a.errs, Error{Pos: token.Position{Filename: "tifa.yaml"}, Msg: "the app has several languages, so site.url must name the scheme and host it is served at, such as https://shop.example.com, which the alternate links to each language need"})
	}
	dirs := make([]string, 0, len(a.locales))
	for dir := range a.locales {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	areas := map[string]Area{}
	for _, dir := range dirs {
		ls := langs.Order(a.locales[dir], def)
		if def != "" && len(ls) > 0 && !slices.Contains(ls, def) {
			a.errs = append(a.errs, Error{Pos: token.Position{Filename: dir + "/" + langs.Dir}, Msg: "the default language " + def + " has no file here: every area has a file for the default language"})
		}
		area := Area{Dir: dir, Languages: ls}
		areas[dir] = area
		m.Areas = append(m.Areas, area)
	}
	// A folder of app/ named like a language would be the first segment of that language's URLs.
	if entries, err := os.ReadDir(filepath.Join(a.root, "app")); err == nil {
		for _, e := range entries {
			if e.IsDir() && site[e.Name()] {
				a.errs = append(a.errs, Error{Pos: token.Position{Filename: "app/" + e.Name()}, Msg: "the folder is named like the language " + e.Name() + ", whose URLs start with it: rename the folder"})
			}
		}
	}
	nearest := func(dir string) (Area, bool) {
		for {
			if area, ok := areas[dir]; ok {
				return area, true
			}
			i := strings.LastIndex(dir, "/")
			if i < 0 {
				return Area{}, false
			}
			dir = dir[:i]
		}
	}
	for _, r := range routes {
		if area, ok := nearest(r.Dir); ok {
			r.Area, r.Languages = area.Dir, area.Languages
		}
	}
	for _, l := range layouts {
		if area, ok := nearest(l.Dir); ok {
			l.Area, l.Languages = area.Dir, area.Languages
		}
	}
	a.site = m.Languages
	return m
}
