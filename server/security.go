package server

import "net/http"

// frameAncestors is the policy every answer carries by default: only pages of the same origin may
// show it in a frame, which stops another site from showing it under its own buttons
// (clickjacking). It changes nothing a page loads.
const frameAncestors = "frame-ancestors 'self'"

// setDefaultHeaders sets the headers every answer carries, before anything of the app sees the
// request, so OnRequest can change or remove them for the paths it chooses. None of them changes
// what a page loads or how it works:
//   - nosniff: a browser takes the Content-Type as it is, so a file that is not a script never
//     runs as one
//   - Referrer-Policy: other sites get only the origin, never the path and query, which may hold
//     a token. It is what current browsers do anyway, said for those that do not.
//   - frameAncestors
func setDefaultHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", frameAncestors)
}
