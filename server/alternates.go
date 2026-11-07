package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"encoding/json/jsontext"
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/core"
)

// The versions of a page in other languages. A page of an area exists in each language of the
// area at the same path under another prefix, unless a Load says otherwise with
// c.SetAlternates. The list goes with the data of the page: into the input of a render, into the
// boot data of a page without SSR and into the answer to a client navigation. The render writes the
// hreflang links from it, and the browser's useAlternates reads it for the language switch.

// pageAlternates is the list as the page of a render gets it.
type pageAlternates struct {
	// Site is site.url without a trailing slash.
	Site string `json:"site,omitempty"`
	// Default is the default language of the site, whose version x-default stands for.
	Default string `json:"default"`
	// Canonical is the path of the page as it was asked for, without the query.
	Canonical string `json:"canonical,omitempty"`
	// List holds a link per version, the page itself included.
	List []alternateLink `json:"list,omitempty"`
	// Custom is true when a Load of the page called c.SetAlternates, so List is the list of the
	// app. Otherwise it is the prefix swap over the languages of the route, which the browser works
	// out for itself, so only Custom lists travel to it.
	Custom bool `json:"custom,omitzero"`
}

type alternateLink struct {
	Language string `json:"language"`
	// URL is the path of the version, with its language prefix.
	URL string `json:"url"`
}

// languagePath returns the path of rest, a path without a language prefix, in language.
func (s *server) languagePath(language, rest string) string {
	if language == s.i18n.Default && !s.i18n.Always {
		return rest
	}
	if rest == "/" {
		return "/" + language
	}
	return "/" + language + rest
}

// alternatesOf works out the versions of the page c answers, once its Loads have returned. It
// returns nil for a site without languages. A page outside every area, a page with one version and
// a page that is not found have a list of fewer than two versions: the render writes no links for
// it.
func (s *server) alternatesOf(c *tifa.Ctx) (*pageAlternates, error) {
	if s.i18n == nil {
		return nil, nil
	}
	site := &pageAlternates{Site: strings.TrimRight(s.i18n.SiteURL, "/"), Default: s.i18n.Default}
	langs := s.langs[c.Pattern()]
	if len(langs) == 0 {
		return site, nil
	}
	language, rest := c.Language(), c.Request().URL.EscapedPath()
	if prefix := "/" + language; rest == prefix || strings.HasPrefix(rest, prefix+"/") {
		if rest = strings.TrimPrefix(rest, prefix); rest == "" {
			rest = "/"
		}
	}
	var list []alternateLink
	given, custom := core.AlternatesOf(c)
	if custom {
		seen := map[string]bool{}
		for _, a := range given {
			switch {
			case !slices.Contains(langs, a.Language):
				return nil, fmt.Errorf("tifa: SetAlternates: language %q is none of the languages of this page %v", a.Language, langs)
			case seen[a.Language]:
				return nil, fmt.Errorf("tifa: SetAlternates: language %q is listed twice", a.Language)
			case !strings.HasPrefix(a.Path, "/") || strings.HasPrefix(a.Path, "//") || strings.ContainsAny(a.Path, "?#<>\"\\ "):
				return nil, fmt.Errorf("tifa: SetAlternates: path %q for %s must be an escaped URL path that starts with one slash, with no query or fragment", a.Path, a.Language)
			}
			seen[a.Language] = true
			list = append(list, alternateLink{Language: a.Language, URL: s.languagePath(a.Language, a.Path)})
		}
	} else {
		for _, l := range langs {
			list = append(list, alternateLink{Language: l, URL: s.languagePath(l, rest)})
		}
	}
	site.Canonical, site.List, site.Custom = s.languagePath(language, rest), list, custom
	return site, nil
}

// alternatesJSON is the list as JSON a script element can hold, nil for a site without languages.
func alternatesJSON(a *pageAlternates) (jsontext.Value, error) {
	if a == nil {
		return nil, nil
	}
	return htmlJSON(a)
}

// carriedJSON is what a page without SSR hands to the browser of the list: the site and the default
// language, and the list when a Load set it. The browser works out the rest.
func carriedJSON(a *pageAlternates) (jsontext.Value, error) {
	if a == nil {
		return nil, nil
	}
	b := pageAlternates{Site: a.Site, Default: a.Default}
	if a.Custom {
		b.List, b.Custom = a.List, true
		if b.List == nil {
			b.List = []alternateLink{}
		}
	}
	return htmlJSON(&b)
}

// navigationJSON is the list a data answer carries: the list of a Load that set one. With clear,
// for an answer of every Load of a page of an area, a page without a list gets {}: the browser
// drops a list it holds for the path, from a Load that set one before, and works the list out.
// Otherwise it is nil, and the browser keeps what it has.
func navigationJSON(a *pageAlternates, clear bool) (jsontext.Value, error) {
	switch {
	case a == nil:
		return nil, nil
	case !a.Custom && clear:
		return jsontext.Value("{}"), nil
	case !a.Custom:
		return nil, nil
	}
	return htmlJSON(struct {
		List []alternateLink `json:"list"`
	}{append([]alternateLink{}, a.List...)})
}

// pageAlternatesFor is the list of the page c answers with status. A page that is not found or
// failed has none.
func (s *server) pageAlternatesFor(c *tifa.Ctx, status int) (*pageAlternates, error) {
	if status != http.StatusOK {
		if s.i18n == nil {
			return nil, nil
		}
		return &pageAlternates{Site: strings.TrimRight(s.i18n.SiteURL, "/"), Default: s.i18n.Default}, nil
	}
	return s.alternatesOf(c)
}

// navigationAlternates is the list a data request answers with: that of a Load that called
// c.SetAlternates in this request. A Load the browser still holds the data of may have set one this
// answer does not know, and the browser keeps the list it has for that path. whole says the
// request ran every Load of the page: then no list is an answer too, see navigationJSON.
func (s *server) navigationAlternates(c *tifa.Ctx, whole bool) (jsontext.Value, error) {
	clear := whole && len(s.langs[c.Pattern()]) > 0
	if _, set := core.AlternatesOf(c); !set && !clear {
		return nil, nil
	}
	a, err := s.alternatesOf(c)
	if err != nil {
		return nil, err
	}
	return navigationJSON(a, clear)
}
