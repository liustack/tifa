// Package config holds the settings of tifa and of the app, and reads them from their sources.
// Runtime precedence from high to low: flags, environment variables, the .env file of the
// working directory, the file named by -c, the project tifa.yaml (written into the generated
// main by tifa build), the defaults. Only the keys of tifa and of the app are read: other
// flags, variables and keys are left alone. Every invalid value fails with its source and key.
//
// A key is lower case words of letters, digits and underscores, at least two of them joined
// with dots, such as server.body_limit. Its flag replaces dots and underscores with hyphens
// (--server-body-limit) and its environment variable is the key in upper case with dots made
// underscores (SERVER_BODY_LIMIT).
package config

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"
)

type Config struct {
	Build   Build
	I18n    I18n
	Site    Site
	Server  Server
	SSR     SSR
	Cache   Cache
	Session Session
}

// I18n is read by the tifa CLI only, from the project tifa.yaml. Default names the default language,
// empty when the app has no more than one. Prefix is "off" or "always", see docs/config.md.
type I18n struct {
	Default string
	Prefix  string
}

// Site is read by the tifa CLI only, from the project tifa.yaml. URL is the scheme and host the
// site is served at, without a trailing slash, empty when not set.
type Site struct {
	URL string
}

// Build is read by the tifa CLI only. It may appear in the project tifa.yaml and nowhere else.
type Build struct {
	Target string
	Public string
	// SkipTanStackCheck builds an app whose TanStack Router is not the version tifa is tested
	// with, see bundle.CheckTanStack.
	SkipTanStackCheck bool
	// DebugInfo keeps the DWARF debug info in the program tifa build makes, for debuggers such
	// as Delve. Without it the program is built with -ldflags=-w.
	DebugInfo bool
	// I18nextCache makes the tifa Vite plugin add a cache of translations to i18next in the SSR
	// bundle, see docs/build.md. It is on by default.
	I18nextCache bool
}

type Server struct {
	Addr      string
	BodyLimit int64
	// ReadTimeout limits the time to read a whole request, body included, and WriteTimeout the
	// time from the end of its header to the end of the answer. A handler that needs longer, an
	// upload or a stream, lifts them for itself with http.ResponseController.
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	// Compress sends text answers with gzip or zstd to clients that accept them, and the
	// browser bundle as the files tifa build compressed.
	Compress bool

	// HTTPS is on when TLSDomains or TLSCert is set, see HTTPS and CheckHTTPS. TLSDomains get
	// certificates from the ACME server at TLSCA (Let's Encrypt), kept in the folder TLSCache.
	// TLSCert and TLSKey are PEM files of the app's own certificate.
	TLSDomains []string
	TLSEmail   string
	TLSCache   string
	TLSCA      string
	TLSCert    string
	TLSKey     string
	// HTTPAddr is the plain HTTP address that answers the http-01 challenges of the ACME server
	// and redirects everything else to HTTPS, "off" for none. It is only used with HTTPS on.
	HTTPAddr string
	// HSTS adds Strict-Transport-Security to the answers of Addr.
	HSTS bool
	// CSP is off, on or report-only: with on, every answer carries CSPPolicy as its
	// Content-Security-Policy, and the documents tifa writes carry it with the hashes of their inline
	// scripts and styles. report-only sends it as Content-Security-Policy-Report-Only instead.
	CSP       string
	CSPPolicy string

	// Writes (every method but GET, HEAD and OPTIONS) that a browser sends from another site are
	// refused with 403, see server.crossSite. TrustedOrigins are the origins that may send
	// them anyway, in the form a browser writes them in the Origin header, such as
	// https://admin.example.com. CrossSitePaths are http.ServeMux patterns, such as
	// POST /auth/apple/callback, whose requests are not checked at all.
	TrustedOrigins []string
	CrossSitePaths []string
}

// HTTPS reports whether Addr serves HTTPS.
func (s Server) HTTPS() bool { return len(s.TLSDomains) > 0 || s.TLSCert != "" || s.TLSKey != "" }

// CheckHTTPS reports settings of HTTPS that do not go together. Load does not call it, since
// a project file may leave out what the deployment adds: serve calls it on the merged settings.
func (s Server) CheckHTTPS() error {
	switch {
	case len(s.TLSDomains) > 0 && (s.TLSCert != "" || s.TLSKey != ""):
		return errors.New("config: server.tls_domains and server.tls_cert both turn on HTTPS, one with certificates from Let's Encrypt, the other with your own files: set only one")
	case s.TLSCert != "" && s.TLSKey == "":
		return errors.New("config: server.tls_cert needs server.tls_key, the file of its private key")
	case s.TLSKey != "" && s.TLSCert == "":
		return errors.New("config: server.tls_key needs server.tls_cert, the file of its certificate")
	case len(s.TLSDomains) > 0 && s.TLSCache == "":
		return errors.New("config: server.tls_domains needs server.tls_cache, a folder that keeps the certificates across restarts, such as /var/lib/app/certs. Without it every start asks Let's Encrypt again and soon hits its limit of 5 certificates a week")
	case len(s.TLSDomains) == 0 && s.TLSEmail != "":
		return errors.New("config: server.tls_email is only used with server.tls_domains")
	case len(s.TLSDomains) == 0 && s.TLSCache != "":
		return errors.New("config: server.tls_cache is only used with server.tls_domains")
	}
	return nil
}

// CheckHTTPSAddr reports HTTPS on the default Addr, which only this machine reaches. It is apart
// from CheckHTTPS because it is about the address the program listens on: serve does not call it
// when systemd passes the socket of server.addr, which is listened on in its place.
func (s Server) CheckHTTPSAddr() error {
	if s.HTTPS() && s.Addr == DefaultAddr {
		return fmt.Errorf("config: HTTPS is on, but server.addr is %s, the default, which only this machine reaches: set it to the address browsers connect to, usually addr: :443", DefaultAddr)
	}
	return nil
}

// LetsEncrypt is the ACME directory of Let's Encrypt, the default server.tls_ca.
const LetsEncrypt = "https://acme-v02.api.letsencrypt.org/directory"

// DefaultCSPPolicy is the policy of server.csp unless server.csp_policy says another. It limits
// scripts to the files of the site and the inline scripts tifa writes, whose hashes each page adds,
// and lets no other site show the pages in a frame. Styles are left alone: React writes style
// attributes, and emotion and MUI add style elements while the page runs.
const DefaultCSPPolicy = "script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'"

type SSR struct {
	VMs      int
	VMMemory int64
	// VMStackDepth is how deep JavaScript calls may nest in each VM, from 1
	// to MaxVMStackDepth.
	VMStackDepth int
	Timeout      time.Duration
	QueueLimit   int
	WaitTimeout  time.Duration
}

type Cache struct {
	Size int64
	// WholePage keeps the pages of routes with Cache for everyone. False renders them like other pages.
	WholePage bool
	// IgnoreQuery lists the query parameters the whole-page cache leaves out of its key and of the
	// URL it renders for: tracking parameters. A name ending in * is a prefix. A page whose Load
	// reads one of them keeps it.
	IgnoreQuery []string
}

// DefaultIgnoreQuery is the default of cache.ignore_query: the tracking parameters of the common
// ad and mail services.
var DefaultIgnoreQuery = []string{"utm_*", "gclid", "gbraid", "wbraid", "fbclid", "msclkid", "twclid", "ttclid", "yclid", "mc_cid", "mc_eid"}

// Session holds the settings of tifa.SetSession. Cookie is the name of the session cookie, MaxAge
// how long a session lasts from the moment SetSession makes it, and MemoryLimit the bytes the
// default in-memory store may hold.
type Session struct {
	Cookie      string
	MaxAge      time.Duration
	MemoryLimit int64
}

// DefaultSessionCookie is the cookie name of a production server. tifa dev uses the name without the __Host- prefix.
const DefaultSessionCookie = "__Host-tifa_session"

// MaxVMStackDepth is the default and the largest ssr.vm_stack_depth: the deepest the engine lets calls
// nest, which internal/ssr checks. Code that recurses without end takes a few KB of stack
// per level before it throws, and the engine does not nest calls deeper than this.
const MaxVMStackDepth = 10000

// DefaultAddr is where a built app and tifa dev listen unless told otherwise.
const DefaultAddr = "127.0.0.1:9527"

// Default holds values sized for small machines. ssr.vms follows GOMAXPROCS, which honours container CPU limits, with no cap.
func Default() Config {
	return Config{
		Build:   Build{Target: "es2020", Public: "public", I18nextCache: true},
		I18n:    I18n{Prefix: "off"},
		Server:  Server{Addr: DefaultAddr, BodyLimit: 1 << 20, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ShutdownTimeout: 10 * time.Second, Compress: true, TLSCA: LetsEncrypt, HTTPAddr: ":80", CSP: "off", CSPPolicy: DefaultCSPPolicy},
		SSR:     SSR{VMs: runtime.GOMAXPROCS(0), VMMemory: 128 << 20, VMStackDepth: MaxVMStackDepth, Timeout: 3 * time.Second, QueueLimit: 32, WaitTimeout: 3 * time.Second},
		Cache:   Cache{Size: 64 << 20, WholePage: true, IgnoreQuery: slices.Clone(DefaultIgnoreQuery)},
		Session: Session{Cookie: DefaultSessionCookie, MaxAge: 30 * 24 * time.Hour, MemoryLimit: 64 << 20},
	}
}

type field struct {
	key   string // section.name as written in files
	help  string
	value value
}

// projectOnly reports whether the setting is read by the tifa CLI from the project tifa.yaml and
// nowhere else: the build settings, the languages of the app and the address of the site.
func (f field) projectOnly() bool {
	return strings.HasPrefix(f.key, "build.") || strings.HasPrefix(f.key, "i18n.") || strings.HasPrefix(f.key, "site.")
}

func fields(c *Config) []field {
	return []field{
		{"build.target", "JavaScript target of the browser bundle", stringValue{&c.Build.Target}},
		{"build.public", "Directory of static files copied as is", stringValue{&c.Build.Public}},
		{"build.skip_tanstack_check", "Build with a TanStack Router version tifa is not tested with", boolValue{&c.Build.SkipTanStackCheck}},
		{"build.debug_info", "Keep the DWARF debug info in the program, for debuggers such as Delve", boolValue{&c.Build.DebugInfo}},
		{"build.i18next_cache", "Keep the strings i18next translates in the SSR bundle and give them again for the same call", boolValue{&c.Build.I18nextCache}},
		{"i18n.default", "Default language of the app, a BCP 47 tag such as zh-CN, needed when the app has several", languageValue{&c.I18n.Default}},
		{"i18n.prefix", "always puts the language in the URL of every route that has languages, the default one too", prefixValue{&c.I18n.Prefix}},
		{"site.url", "Scheme and host the site is served at, such as https://shop.example.com", siteURLValue{&c.Site.URL}},
		{"server.addr", "HTTP listen address", stringValue{&c.Server.Addr}},
		{"server.body_limit", "Request body limit", sizeValue{&c.Server.BodyLimit, 1}},
		{"server.read_timeout", "Time to read a whole request, body included", durationValue{&c.Server.ReadTimeout}},
		{"server.write_timeout", "Time from the end of a request header to the end of the answer", durationValue{&c.Server.WriteTimeout}},
		{"server.shutdown_timeout", "Time allowed for in-flight requests, and then for OnShutdown, after a stop signal", durationValue{&c.Server.ShutdownTimeout}},
		{"server.compress", "Compress text answers and send the compressed browser bundle", boolValue{&c.Server.Compress}},
		{"server.tls_domains", "Serve HTTPS with Let's Encrypt certificates for these comma-separated domains", domainsValue{&c.Server.TLSDomains}},
		{"server.tls_email", "Contact address given to Let's Encrypt, optional", optionalValue{&c.Server.TLSEmail}},
		{"server.tls_cache", "Folder that keeps the certificates of tls_domains across restarts", optionalValue{&c.Server.TLSCache}},
		{"server.tls_ca", "ACME directory URL that tls_domains get their certificates from", caValue{&c.Server.TLSCA}},
		{"server.tls_cert", "Serve HTTPS with this certificate file (PEM), reloaded when it changes", optionalValue{&c.Server.TLSCert}},
		{"server.tls_key", "Private key file (PEM) of tls_cert", optionalValue{&c.Server.TLSKey}},
		{"server.http_addr", "With HTTPS, plain HTTP address that answers Let's Encrypt and redirects to HTTPS, off for none", httpAddrValue{&c.Server.HTTPAddr}},
		{"server.hsts", "Send Strict-Transport-Security, so browsers keep to HTTPS for a year", boolValue{&c.Server.HSTS}},
		{"server.csp", "Send server.csp_policy as the Content-Security-Policy: off, on or report-only", cspValue{&c.Server.CSP}},
		{"server.csp_policy", "The Content-Security-Policy of server.csp, to which pages add the hashes of their inline scripts", policyValue{&c.Server.CSPPolicy}},
		{"server.trusted_origins", "Comma-separated origins whose pages may send cross-site writes, such as https://admin.example.com", originsValue{&c.Server.TrustedOrigins}},
		{"server.cross_site_paths", "Comma-separated path patterns, such as POST /auth/callback, that accept cross-site writes", patternsValue{&c.Server.CrossSitePaths}},
		{"ssr.vms", "Prewarmed JavaScript VMs, defaults to GOMAXPROCS (the CPU limit in containers)", intValue{p: &c.SSR.VMs, min: 1}},
		{"ssr.vm_memory", "Memory limit of each VM", sizeValue{&c.SSR.VMMemory, 1}},
		{"ssr.vm_stack_depth", "How deep JavaScript calls may nest in each VM, at most 10000", intValue{p: &c.SSR.VMStackDepth, min: 1, max: MaxVMStackDepth,
			why: "the engine does not nest calls deeper"}},
		{"ssr.timeout", "Render time budget of each request", durationValue{&c.SSR.Timeout}},
		{"ssr.queue_limit", "Requests allowed to wait for a VM, 0 rejects when all VMs are busy", intValue{p: &c.SSR.QueueLimit, min: 0}},
		{"ssr.wait_timeout", "Maximum time waiting for a VM before answering 503", durationValue{&c.SSR.WaitTimeout}},
		{"cache.size", "Rendered page cache size, 0 disables it", sizeValue{&c.Cache.Size, 0}},
		{"cache.whole_page", "Keep the pages of routes with Cache for everyone, false renders them like other pages", boolValue{&c.Cache.WholePage}},
		{"cache.ignore_query", "Tracking query parameters the whole-page cache ignores: names are added to the list, -name takes one out, -* empties it", ignoreQueryValue{&c.Cache.IgnoreQuery}},
		{"session.cookie", "Name of the session cookie", cookieNameValue{&c.Session.Cookie}},
		{"session.max_age", "How long a session lasts, from the moment it is set", ageValue{&c.Session.MaxAge}},
		{"session.memory_limit", "Bytes the default in-memory session store may hold", sizeValue{&c.Session.MemoryLimit, 1}},
	}
}

func runtimeFields(c *Config) []field {
	return slices.DeleteFunc(fields(c), field.projectOnly)
}

// Keys lists the key of every setting of tifa as files write it, such as server.addr, build keys first.
func Keys() []string {
	var keys []string
	for _, f := range fields(&Config{}) {
		keys = append(keys, f.key)
	}
	return keys
}

// Load merges the runtime settings of tifa: settings holds the runtime keys of the project
// tifa.yaml as text, which tifa build checked and wrote into the generated main, and s the
// sources the program was started with. from gives the source of the value of each key that
// is not at its default, such as "env SSR_VMS". Every invalid value is reported, each with its
// source and key.
func Load(s *Sources, settings map[string]string) (c Config, from map[string]string, err error) {
	c = Default()
	from = map[string]string{}
	var errs []error
	for _, f := range runtimeFields(&c) {
		label, ferrs := s.apply(f.key, f.value, settings)
		if label != "" {
			from[f.key] = label
		}
		errs = append(errs, ferrs...)
	}
	return c, from, errors.Join(errs...)
}

// Project reads the project tifa.yaml. It returns the settings it gives, build settings
// included, over the defaults, and the runtime keys it sets as text, which the generated main
// hands to Load. Keys that are not settings of tifa are left alone.
func Project(data []byte) (Config, map[string]string, error) {
	c := Default()
	entries, err := yamlEntries(data)
	if err != nil {
		return c, nil, fmt.Errorf("config: tifa.yaml: %w", err)
	}
	settings := map[string]string{}
	var errs []error
	for _, f := range fields(&c) {
		e, ok := entries[f.key]
		if !ok {
			continue
		}
		where := fmt.Sprintf("tifa.yaml line %d", e.line)
		if err := e.check(f.value.kind(), false); err != nil {
			errs = append(errs, fmt.Errorf("config: %s: %s: %w", where, f.key, err))
			continue
		}
		if err := f.value.Set(e.raw); err != nil {
			errs = append(errs, fmt.Errorf("config: %s: %s: %w", where, f.key, err))
			continue
		}
		if !f.projectOnly() {
			settings[f.key] = e.raw
		}
	}
	return c, settings, errors.Join(errs...)
}
