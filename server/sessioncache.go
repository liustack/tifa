package server

import (
	"fmt"
	"net/http"
	"strings"
)

// An answer that carries the session is meant for one visitor. Its Cache-Control must keep every
// shared cache from storing it: no-store, or private and no-cache together without field lists.
// An app that sets anything else gets an unexpected error instead of the answer.

// sessionCacheError is the error of an answer that carries the session with a Cache-Control the
// app made shareable. Server.fail answers it as no-store.
type sessionCacheError struct{ value string }

func (e *sessionCacheError) Error() string {
	return fmt.Sprintf("tifa: the answer carries the session, but its Cache-Control is %q: set no-store, or private together with no-cache, or leave Cache-Control alone", e.value)
}

// checkSessionCache reports a *sessionCacheError when the headers of an answer that carries the
// session are not safe, and clears the headers that conflict: Cache-Control, ETag and Last-Modified.
func checkSessionCache(h http.Header) error {
	if cacheControlHoldsSession(h) {
		return nil
	}
	err := &sessionCacheError{value: strings.Join(h.Values("Cache-Control"), ", ")}
	h.Del("Cache-Control")
	h.Del("ETag")
	h.Del("Last-Modified")
	return err
}

// cacheControlHoldsSession merges the Cache-Control fields (RFC 9111 section 5.2) and reads the
// directives. It is true for no-store, and for private and no-cache both without arguments. A
// private or no-cache with a list of fields only covers those fields, so it does not count. A
// malformed list and must-understand are never accepted. No header is fine: the answer is then
// private, no-cache by default.
func cacheControlHoldsSession(h http.Header) bool {
	values := h.Values("Cache-Control")
	if len(values) == 0 {
		return true
	}
	var store, private, noCache bool
	for _, value := range values {
		directives, ok := splitDirectives(value)
		if !ok {
			return false
		}
		for _, d := range directives {
			name, arg, hasArg := strings.Cut(d, "=")
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" || strings.ContainsAny(name, " \t\"") {
				return false
			}
			switch name {
			case "must-understand":
				return false
			case "no-store":
				store = true
			case "private":
				private = private || !hasArg
			case "no-cache":
				noCache = noCache || !hasArg
			}
			_ = arg
		}
	}
	return store || private && noCache
}

// splitDirectives splits a Cache-Control field at the commas that are outside quotes. It reports
// false for an unclosed quote or an empty directive.
func splitDirectives(value string) ([]string, bool) {
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			if quoted {
				i++
			}
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				out = append(out, value[start:i])
				start = i + 1
			}
		}
	}
	if quoted {
		return nil, false
	}
	out = append(out, value[start:])
	for i, d := range out {
		out[i] = strings.TrimSpace(d)
		if out[i] == "" {
			return nil, false
		}
	}
	return out, true
}
