package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/ssr"
)

// frameAncestors is the policy every answer carries by default: only pages of the same origin may
// show it in a frame, which stops another site from showing it under its own buttons
// (clickjacking). It changes nothing a page loads.
const frameAncestors = "frame-ancestors 'self'"

// The headers of a Content-Security-Policy: the one browsers enforce, and the one they only report.
const (
	cspHeader       = "Content-Security-Policy"
	cspReportHeader = "Content-Security-Policy-Report-Only"
)

// setDefaultHeaders sets the headers every answer carries, before anything of the app sees the
// request, so OnRequest can change or remove them for the paths it chooses. None of them changes
// what a page loads or how it works:
//   - nosniff: a browser takes the Content-Type as it is, so a file that is not a script never
//     runs as one
//   - Referrer-Policy: other sites get only the origin, never the path and query, which may hold
//     a token. It is what current browsers do anyway, said for those that do not.
//   - frameAncestors, which server.csp on replaces with server.csp_policy
//
// With server.csp the policy goes out as the header of policyHeader, which the documents tifa
// writes replace with the policy and their hashes (setPagePolicy).
func (s *server) setDefaultHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set(cspHeader, frameAncestors)
	if name := s.policyHeader(); name != "" {
		h.Set(name, s.cfg.CSPPolicy)
	}
}

// policyHeader is the header that carries server.csp_policy, and "" with server.csp off.
func (s *server) policyHeader() string {
	switch s.cfg.CSP {
	case "on":
		return cspHeader
	case "report-only":
		return cspReportHeader
	}
	return ""
}

// policyKey is what the policy of the documents follows from besides their HTML: the header and the
// policy, "" with server.csp off. The ETag of a page covers it (renderKey, contentTag), so a
// browser that holds a page under another policy gets the page again with this one.
func (s *server) policyKey() string {
	if name := s.policyHeader(); name != "" {
		return name + ": " + s.cfg.CSPPolicy
	}
	return ""
}

// renderedPolicy is the policy of a document the bundle rendered for in: the hashes of what the
// render marked (page), and of the inline scripts and styles of the session head and of the head of
// the build, which Go wrote and the render copies into the page as they are. "" with server.csp
// off.
func (s *server) renderedPolicy(f *front, in ssr.Input, page ssr.Page) string {
	if s.policyHeader() == "" {
		return ""
	}
	scripts, styles := ssr.InlineHashes(in.SessionHead)
	scripts = append(append(scripts, f.headScripts...), page.Scripts...)
	styles = append(append(styles, f.headStyles...), page.Styles...)
	return withHashes(s.cfg.CSPPolicy, scripts, styles)
}

// documentPolicy is the policy of doc, a document Go wrote whole, which holds no markup of data:
// the hashes of every inline script and style in it. "" with server.csp off.
func (s *server) documentPolicy(doc string) string {
	if s.policyHeader() == "" {
		return ""
	}
	scripts, styles := ssr.InlineHashes(doc)
	return withHashes(s.cfg.CSPPolicy, scripts, styles)
}

// setPagePolicy puts policy, the policy of a document with its hashes, in place of the policy
// setDefaultHeaders set. A header OnRequest, a Load or a Layout changed stays as the app set it, and
// an empty policy (server.csp off) changes nothing.
func (s *server) setPagePolicy(h http.Header, policy string) {
	if name := s.policyHeader(); policy != "" && s.holdsBasePolicy(h, name) {
		h.Set(name, policy)
	}
}

// dropPagePolicy takes the policy setDefaultHeaders set off the 304 of a page. The 304 is answered
// before the render, so tifa does not know the hashes of the page there, and a browser updates the
// headers of its copy from the headers of a 304 (RFC 9111 section 4.3.4): Firefox does it for the
// policy too, and Chromium has kept it from that only for some versions (it used to skip every
// header that starts with content-). The policy without the hashes would then block the inline
// scripts of the copy the next time it is shown, so the 304 carries none, and the browser keeps
// the policy that came with the copy, the policy for those bytes. A policy the app set is the same
// on the 200 and the 304, and stays.
func (s *server) dropPagePolicy(h http.Header) {
	if name := s.policyHeader(); name != "" && s.holdsBasePolicy(h, name) {
		h.Del(name)
	}
}

func (s *server) holdsBasePolicy(h http.Header, name string) bool {
	values := h.Values(name)
	return len(values) == 1 && values[0] == s.cfg.CSPPolicy
}

// withHashes adds the hashes of the inline scripts and styles of a document to policy, each once,
// in the directives that decide whether they run:
//   - script-src and script-src-elem, and style-src and style-src-elem for styles
//   - without either, default-src, written as a script-src or style-src of its own with the sources
//     of default-src, which then applies to them as before
//
// A directive with 'unsafe-inline' gets none, since a browser ignores 'unsafe-inline' once a
// directive lists a hash, and the inline elements of the app would stop running. 'none' gets none
// either. Without any of the directives the policy does not limit them, and stays as it is.
func withHashes(policy string, scripts, styles []string) string {
	directives := strings.Split(policy, ";")
	for i, d := range directives {
		directives[i] = strings.TrimSpace(d)
	}
	directives = slices.DeleteFunc(directives, func(d string) bool { return d == "" })
	for _, kind := range []struct {
		name   string
		hashes []string
	}{{"script", scripts}, {"style", styles}} {
		if len(kind.hashes) == 0 {
			continue
		}
		found := false
		for i, d := range directives {
			if name := directiveName(d); name == kind.name+"-src" || name == kind.name+"-src-elem" {
				found = true
				directives[i] = addSources(d, kind.hashes)
			}
		}
		if found {
			continue
		}
		for _, d := range directives {
			if directiveName(d) == "default-src" {
				_, sources, _ := strings.Cut(d, " ")
				own := kind.name + "-src " + strings.TrimSpace(sources)
				if added := addSources(own, kind.hashes); added != own {
					directives = append(directives, added)
				}
				break
			}
		}
	}
	return strings.Join(directives, "; ")
}

func directiveName(d string) string {
	name, _, _ := strings.Cut(d, " ")
	return strings.ToLower(name)
}

// addSources adds the hashes to the directive d that it does not list, unless it has
// 'unsafe-inline' or is 'none'.
func addSources(d string, hashes []string) string {
	fields := strings.Fields(d)
	for _, f := range fields[1:] {
		if strings.EqualFold(f, "'unsafe-inline'") {
			return d
		}
	}
	if len(fields) == 2 && strings.EqualFold(fields[1], "'none'") {
		return d
	}
	var b strings.Builder
	b.WriteString(strings.Join(fields, " "))
	for _, h := range hashes {
		if !slices.Contains(fields, h) {
			fields = append(fields, h)
			b.WriteString(" " + h)
		}
	}
	return b.String()
}
