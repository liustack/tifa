package server

import (
	"fmt"
	"net/http"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/core"
)

// crossSiteProtection is the check every request passes first. A write (any method but GET, HEAD
// and OPTIONS) that a browser sends from another site is refused: Sec-Fetch-Site says where the
// page came from, and a browser too old to send it is judged by comparing its Origin with the
// Host. A request with neither header comes from something that is not a browser, such as curl or
// a webhook, and passes. The settings are checked when they are loaded, so a bad one here is a
// bug in the caller.
func crossSiteProtection(cfg config.Server) *http.CrossOriginProtection {
	p := http.NewCrossOriginProtection()
	for _, origin := range cfg.TrustedOrigins {
		if err := p.AddTrustedOrigin(origin); err != nil {
			panic(fmt.Sprintf("tifa: server.trusted_origins: %v", err))
		}
	}
	for _, pattern := range cfg.CrossSitePaths {
		p.AddInsecureBypassPattern(pattern)
	}
	return p
}

// refuseCrossSite logs a request that crossSite refused and answers it with 403 and the reason, in the JSON
// every other error of the app has. The request goes no further: OnRequest does not see it and no
// route runs. No hook has set the headers of the answer, so it is marked not to be stored: a cache
// must not hand it to the same-origin request that comes next.
func (s *Server) refuseCrossSite(w http.ResponseWriter, r *http.Request, reason error) {
	// A line for each refusal, with the headers the check read, tells a proxy that rewrites Host
	// from a real attack.
	core.Logger().WarnContext(r.Context(), "tifa: refused a cross-site write", "method", r.Method, "path", r.URL.Path,
		"reason", reason.Error(), "origin", r.Header.Get("Origin"), "sec_fetch_site", r.Header.Get("Sec-Fetch-Site"), "host", r.Host)
	tw := &writer{ResponseWriter: w, plain: !s.cfg.Compress}
	c := core.New(tw, r, "", s.cfg.BodyLimit)
	defer func() { _ = core.Finish(c) }()
	w.Header().Set("Cache-Control", "no-store")
	s.fail(c, tw, tifa.Error{Status: http.StatusForbidden, Message: reason.Error()}, false)
}
