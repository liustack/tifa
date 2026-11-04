package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/config"
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
// A directive whose 'unsafe-inline' lets every inline element run (allowsAllInline) gets none: a
// hash would turn 'unsafe-inline' off, and the inline elements of the app would stop running.
// 'none' gets none either. Without any of the directives the policy does not limit them, and stays as it is. The
// policy is read as browsers read it (config.PolicyDirectives) and written back with one space
// between the sources of a directive.
func withHashes(policy string, scripts, styles []string) string {
	directives := config.PolicyDirectives(policy)
	for _, kind := range []struct {
		name   string
		hashes []string
	}{{"script", scripts}, {"style", styles}} {
		if len(kind.hashes) == 0 {
			continue
		}
		found := false
		for i, d := range directives {
			if name := strings.ToLower(d[0]); name == kind.name+"-src" || name == kind.name+"-src-elem" {
				found = true
				directives[i] = addSources(d, kind.name, kind.hashes)
			}
		}
		if found {
			continue
		}
		for _, d := range directives {
			if strings.ToLower(d[0]) == "default-src" {
				own := append([]string{kind.name + "-src"}, d[1:]...)
				if added := addSources(own, kind.name, kind.hashes); len(added) > len(own) {
					directives = append(directives, added)
				}
				break
			}
		}
	}
	written := make([]string, len(directives))
	for i, d := range directives {
		written[i] = strings.Join(d, " ")
	}
	return strings.Join(written, "; ")
}

// addSources adds the hashes to the directive d, its name and sources, for elements of kind
// (script or style), that it does not list, unless its 'unsafe-inline' lets every inline element
// run (allowsAllInline) or it is 'none'.
func addSources(d []string, kind string, hashes []string) []string {
	if allowsAllInline(d[1:], kind) {
		return d
	}
	if len(d) == 2 && strings.EqualFold(d[1], "'none'") {
		return d
	}
	d = slices.Clip(d)
	for _, h := range hashes {
		if !slices.Contains(d, h) {
			d = append(d, h)
		}
	}
	return d
}

// allowsAllInline reports whether sources let every inline element of kind (script or style) run,
// as CSP3 decides it (https://www.w3.org/TR/CSP3/#allow-all-inline): they have 'unsafe-inline' and
// no hash or nonce, and for scripts no 'strict-dynamic'. Otherwise a browser ignores
// 'unsafe-inline', and inline elements run only by a hash or a nonce.
func allowsAllInline(sources []string, kind string) bool {
	unsafeInline := false
	for _, s := range sources {
		s = strings.ToLower(s)
		switch {
		case s == "'unsafe-inline'":
			unsafeInline = true
		case isNonceOrHash(s):
			return false
		case s == "'strict-dynamic'" && kind == "script":
			return false
		}
	}
	return unsafeInline
}

// isNonceOrHash reports whether the source s, in lower case, is a nonce-source or a hash-source by
// the grammar of CSP3 (https://www.w3.org/TR/CSP3/#grammardef-nonce-source): 'nonce-, or sha256-,
// sha384- or sha512- after the quote, then a base64-value and the closing quote. Browsers ignore
// any other source, so it turns no 'unsafe-inline' off.
func isNonceOrHash(s string) bool {
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return false
	}
	inner := s[1 : len(s)-1]
	for _, prefix := range []string{"nonce-", "sha256-", "sha384-", "sha512-"} {
		if value, found := strings.CutPrefix(inner, prefix); found {
			return isBase64Value(value)
		}
	}
	return false
}

// isBase64Value reports whether v is a base64-value of CSP3: at least one letter, digit, +, /, -
// or _, then at most two =.
func isBase64Value(v string) bool {
	body := strings.TrimRight(v, "=")
	if body == "" || len(v)-len(body) > 2 {
		return false
	}
	for _, c := range []byte(body) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
