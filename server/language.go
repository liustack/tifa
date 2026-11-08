package server

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/core"
)

// The language of a request is the first segment of its path when that is a language of the site.
// The server takes it off before it matches the path, so ServeMux only sees plain paths, and
// puts the URL as it came back once the route is known: everything after the match, the render key, the URL SSR gets, the Ctx and the redirects of the app, sees the public URL.
//
//   - A route without languages (outside every area) has the default language and no prefix. A
//     prefix on its URL is a 404.
//   - A prefix the route has no language for is a 404.
//   - The default language with a prefix is a 308 to the URL without it, unless i18n.prefix is
//     always, which makes the URL without a prefix a 308 to the one with it, for every route that
//     has languages.

// langInfo is what the server knows of the language of one request, kept in its context.
type langInfo struct {
	// lang is the language of the request: the prefix, else the default language.
	lang string
	// prefixed reports whether the URL starts with a language, and rest is its path without it,
	// as it is escaped, "/" for the prefix alone.
	prefixed bool
	rest     string
	// spelled is true when the prefix is written with escapes, which answers a 308 to the plain one.
	spelled bool
	// public is the URL as it came, with the query of the data request taken off.
	public *url.URL
}

type langKey struct{}

// langInfoOf returns what splitLanguage found, nil on a site without languages.
func langInfoOf(r *http.Request) *langInfo {
	info, _ := r.Context().Value(langKey{}).(*langInfo)
	return info
}

// setI18n gives the server the languages of the app. It is called before the first request.
func (s *server) setI18n(i *I18n) {
	s.i18n = i
	s.known = map[string]bool{}
	if i != nil {
		for _, l := range i.Languages {
			s.known[l] = true
		}
	}
}

// splitLanguage returns r with the language segment taken off its path, for ServeMux to match,
// and the language of the request in its context (see langInfoOf). It returns r itself on a site
// without languages.
func (s *server) splitLanguage(r *http.Request) *http.Request {
	if s.i18n == nil {
		return r
	}
	info := &langInfo{lang: s.i18n.Default, public: r.URL}
	escaped := r.URL.EscapedPath()
	if seg, after, ok := strings.Cut(strings.TrimPrefix(escaped, "/"), "/"); strings.HasPrefix(escaped, "/") && s.known[unescapedSegment(seg)] {
		// ServeMux decodes a parameter segment, so the spelling en%2DUS names the language too.
		info.lang, info.prefixed, info.rest = unescapedSegment(seg), true, "/"
		info.spelled = seg != info.lang
		if ok {
			info.rest += after
		}
	}
	out := r.WithContext(context.WithValue(r.Context(), langKey{}, info))
	if !info.prefixed {
		return out
	}
	path, err := url.PathUnescape(info.rest)
	if err != nil {
		path = info.rest // the URL parsed, so this does not happen
	}
	u := *r.URL
	u.Path, u.RawPath = path, ""
	if (&url.URL{Path: path}).EscapedPath() != info.rest {
		u.RawPath = info.rest
	}
	out.URL = &u
	return out
}

// restorePublic puts the URL as it came back on r, which ServeMux matched with the prefix taken
// off. The path values it found stay.
func restorePublic(r *http.Request) {
	if info := langInfoOf(r); info != nil {
		u := *info.public
		r.URL = &u
	}
}

// languageMove decides what a request does with the languages of its route: the location of a
// 308 it is answered with, whether it is a 404, or neither, and then the language of the request is
// info.lang. langs are the languages of the route, none outside every area.
func (s *server) languageMove(info *langInfo, langs []string) (location string, notFound bool) {
	switch {
	case info.prefixed && !slices.Contains(langs, info.lang):
		return "", true
	case info.prefixed && info.lang == s.i18n.Default && !s.i18n.Always:
		return withQuery(info.rest, info.public.RawQuery), false
	case !info.prefixed && len(langs) > 0 && s.i18n.Always:
		path := info.public.EscapedPath()
		if path == "/" {
			path = ""
		}
		return withQuery("/"+s.i18n.Default+path, info.public.RawQuery), false
	}
	return "", false
}

func withQuery(path, query string) string {
	if query == "" {
		return path
	}
	return path + "?" + query
}

// answerLanguage answers a request whose route has the languages langs when the language of the
// URL asks for a 308 or a 404, and reports whether it did. It is called with the URL as it came.
func (s *server) answerLanguage(w http.ResponseWriter, r *http.Request, langs []string) bool {
	info := langInfoOf(r)
	if info == nil {
		return false
	}
	location, notFound := s.languageMove(info, langs)
	switch {
	case notFound:
		// The 404 of the URL as if no route matched, in the language of the prefix.
		routeHandler{s: s}.ServeHTTP(w, r)
		return true
	case location != "":
		redirect := &core.RedirectError{URL: location, Status: http.StatusPermanentRedirect}
		s.serve(w, r, "", false, func(*tifa.Ctx) error { return redirect })
		return true
	}
	return false
}

// withLanguage gives location, the URL of a redirect of the app as http.Redirect resolved it, the
// prefix of the language of r when it is a path of the site whose route has that language, as a
// link gets it in the browser. A path that starts with a language, a file serveStatic answers,
// a path whose route lacks the language or matches none, and a URL of another site stay as written.
func (s *server) withLanguage(r *http.Request, location string) string {
	info := langInfoOf(r)
	if info == nil || info.lang == s.i18n.Default && !s.i18n.Always || !strings.HasPrefix(location, "/") || strings.HasPrefix(location, "//") {
		return location
	}
	u, err := url.Parse(location)
	if err != nil {
		return location
	}
	if seg, _, _ := strings.Cut(strings.TrimPrefix(u.EscapedPath(), "/"), "/"); s.known[unescapedSegment(seg)] || s.isFile(u.Path) {
		return location
	}
	h, _ := s.mux.Handler(&http.Request{Method: http.MethodGet, Host: r.Host, URL: &url.URL{Path: u.Path, RawPath: u.RawPath}})
	if route, ok := h.(routeHandler); !ok || !slices.Contains(s.langs[route.pattern], info.lang) {
		return location
	}
	// The home of a language has no trailing slash: / is /en-US.
	if u.Path == "/" {
		return "/" + info.lang + location[1:]
	}
	return "/" + info.lang + location
}

// hasLanguage reports whether the 404 of a URL may show inside the Layout: a URL with a language
// prefix needs a Layout of that language.
func (s *server) hasLanguage(layout string, info *langInfo) bool {
	return !info.prefixed || slices.Contains(s.layoutLangs[layout], info.lang)
}

// unescapedSegment decodes one segment of an escaped path. A segment that does not decode, or that
// decodes to something with a slash, is no language and comes back with a NUL, which none is.
func unescapedSegment(seg string) string {
	v, err := url.PathUnescape(seg)
	if err != nil || strings.ContainsAny(v, "/\x00") {
		return "\x00"
	}
	return v
}
