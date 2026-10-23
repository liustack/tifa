// Package config loads tifa settings from layered sources.
// Runtime precedence from high to low: flags, TIFA_* environment variables, the file named by --config,
// the tifa.yaml embedded at build time, framework defaults. Any invalid value fails with its layer and key.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"go.yaml.in/yaml/v3"
)

type Config struct {
	Build  Build
	Server Server
	SSR    SSR
	Cache  Cache
}

// Build is read by the tifa CLI only. It may appear in the project tifa.yaml and nowhere else.
type Build struct {
	Target string
	Public string
	// SkipTanStackCheck builds an app whose TanStack Router is not the version tifa is tested
	// with, see bundle.CheckTanStack.
	SkipTanStackCheck bool
	// PGO is the profile go build optimizes the program with: auto, off, or a file relative to
	// the app root. auto is the app's default.pgo, or else tifa's profile of page renders in an
	// app with pages.
	PGO string
	// DebugInfo keeps the DWARF debug info in the program tifa build makes, for debuggers such
	// as Delve. Without it the program is built with -ldflags=-w.
	DebugInfo bool
	// I18nextCache makes the tifa Vite plugin add a cache of translations to i18next in the SSR
	// bundle, see docs/build.md.
	I18nextCache bool
}

type Server struct {
	Addr            string
	BodyLimit       int64
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
	case s.HTTPS() && s.Addr == DefaultAddr:
		return fmt.Errorf("config: HTTPS is on, but server.addr is %s, the default, which only this machine reaches: set it to the address browsers connect to, usually addr: :443", DefaultAddr)
	}
	return nil
}

// LetsEncrypt is the ACME directory of Let's Encrypt, the default server.tls_ca.
const LetsEncrypt = "https://acme-v02.api.letsencrypt.org/directory"

type SSR struct {
	VMs      int
	VMMemory int64
	// VMStackDepth is how deep JavaScript calls may nest in each VM, in the engine's stack slots,
	// about one per call, from 1 to MaxVMStackDepth.
	VMStackDepth int
	Timeout      time.Duration
	QueueLimit   int
	WaitTimeout  time.Duration
}

type Cache struct {
	Size int64
}

// MaxVMStackDepth is the default and the largest ssr.vm_stack_depth: the engine's own default,
// which internal/ssr checks. Code that recurses without end takes about 1 KB of goroutine
// stack per level before it throws, and much deeper than this it would pass Go's stack limit,
// which ends the process.
const MaxVMStackDepth = 10000

// DefaultAddr is where a built app and tifa dev listen unless told otherwise.
const DefaultAddr = "127.0.0.1:9527"

// Default holds values sized for small machines. ssr.vms follows GOMAXPROCS, which honours container CPU limits, with no cap.
func Default() Config {
	return Config{
		Build:  Build{Target: "es2020", Public: "public", PGO: "auto"},
		Server: Server{Addr: DefaultAddr, BodyLimit: 1 << 20, ShutdownTimeout: 10 * time.Second, Compress: true, TLSCA: LetsEncrypt, HTTPAddr: ":80"},
		SSR:    SSR{VMs: runtime.GOMAXPROCS(0), VMMemory: 128 << 20, VMStackDepth: MaxVMStackDepth, Timeout: 3 * time.Second, QueueLimit: 32, WaitTimeout: time.Second},
		Cache:  Cache{Size: 64 << 20},
	}
}

type field struct {
	key   string // section.name as written in files
	help  string
	value value
}

func (f field) build() bool { return strings.HasPrefix(f.key, "build.") }

func (f field) flag() string { return strings.NewReplacer(".", "-", "_", "-").Replace(f.key) }

func (f field) env() string {
	return "TIFA_" + strings.ToUpper(strings.ReplaceAll(f.key, ".", "_"))
}

func fields(c *Config) []field {
	return []field{
		{"build.target", "JavaScript target of the browser bundle", stringValue{&c.Build.Target}},
		{"build.public", "Directory of static files copied as is", stringValue{&c.Build.Public}},
		{"build.skip_tanstack_check", "Build with a TanStack Router version tifa is not tested with", boolValue{&c.Build.SkipTanStackCheck}},
		{"build.pgo", "Profile for profile-guided optimization: auto, off or a file", pgoValue{&c.Build.PGO}},
		{"build.debug_info", "Keep the DWARF debug info in the program, for debuggers such as Delve", boolValue{&c.Build.DebugInfo}},
		{"build.i18next_cache", "Keep the strings i18next translates in the SSR bundle and give them again for the same call", boolValue{&c.Build.I18nextCache}},
		{"server.addr", "HTTP listen address", stringValue{&c.Server.Addr}},
		{"server.body_limit", "Request body limit", sizeValue{&c.Server.BodyLimit, 1}},
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
		{"ssr.vms", "Prewarmed JavaScript VMs, defaults to GOMAXPROCS (the CPU limit in containers)", intValue{p: &c.SSR.VMs, min: 1}},
		{"ssr.vm_memory", "Memory limit of each VM", sizeValue{&c.SSR.VMMemory, 1}},
		{"ssr.vm_stack_depth", "How deep JavaScript calls may nest in each VM, at most 10000", intValue{p: &c.SSR.VMStackDepth, min: 1, max: MaxVMStackDepth,
			why: "a deeper stack lets code that recurses without end grow the goroutine stack past Go's limit, which ends the process"}},
		{"ssr.timeout", "Render time budget of each request", durationValue{&c.SSR.Timeout}},
		{"ssr.queue_limit", "Requests allowed to wait for a VM, 0 rejects when all VMs are busy", intValue{p: &c.SSR.QueueLimit, min: 0}},
		{"ssr.wait_timeout", "Maximum time waiting for a VM before answering 503", durationValue{&c.SSR.WaitTimeout}},
		{"cache.size", "Rendered page cache size, 0 disables it", sizeValue{&c.Cache.Size, 0}},
	}
}

// Keys lists the key of every setting as files write it, such as server.addr, build keys first.
func Keys() []string {
	var keys []string
	for _, f := range fields(&Config{}) {
		keys = append(keys, f.key)
	}
	return keys
}

// AddFlags registers --config and one flag per runtime setting, such as --server-addr.
func AddFlags(fs *pflag.FlagSet) {
	fs.String("config", "", "Deployment config file (.yaml or .json)")
	d := Default()
	for _, f := range fields(&d) {
		if !f.build() {
			fs.Var(&rawValue{typ: f.value.kind()}, f.flag(), fmt.Sprintf("%s (default %s)", f.help, f.value))
		}
	}
}

// Load merges the runtime settings. fs must have been set up by AddFlags and parsed.
// environ is in os.Environ form. embeddedName is tifa.yaml or tifa.json, empty when the project has no file.
func Load(fs *pflag.FlagSet, environ []string, embeddedName string, embedded []byte) (Config, error) {
	c := Default()
	all := fields(&c)
	if embeddedName != "" {
		if err := applyFile(all, "embedded "+embeddedName, embeddedName, embedded, true); err != nil {
			return c, err
		}
	}
	if path, _ := fs.GetString("config"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("config: %w", err)
		}
		if err := applyFile(all, "file "+filepath.Base(path), path, data, false); err != nil {
			return c, err
		}
	}
	runtimeFields := slices.DeleteFunc(slices.Clone(all), field.build)
	if err := applyEnv(runtimeFields, environ); err != nil {
		return c, err
	}
	var err error
	fs.Visit(func(fl *pflag.Flag) {
		for _, f := range runtimeFields {
			if err == nil && fl.Name == f.flag() {
				err = set(f, "flag --"+fl.Name, fl.Value.String())
			}
		}
	})
	return c, err
}

func set(f field, layer, raw string) error {
	if err := f.value.Set(raw); err != nil {
		return fmt.Errorf("config: %s: %s: %w", layer, f.key, err)
	}
	return nil
}

func applyEnv(fs []field, environ []string) error {
	environ = slices.Sorted(slices.Values(environ))
	for _, kv := range environ {
		name, raw, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, "TIFA_") {
			continue
		}
		i := slices.IndexFunc(fs, func(f field) bool { return f.env() == name })
		if i < 0 {
			return fmt.Errorf("config: env %s: unknown key", name)
		}
		if err := set(fs[i], "env "+name, raw); err != nil {
			return err
		}
	}
	return nil
}

// entry is one setting read from a file. typ is the type the file gave the value: string, number, bool or null.
// line is 0 when the format has no line numbers.
type entry struct {
	key, raw, typ string
	line          int
	err           error
}

func applyFile(fs []field, layer, name string, data []byte, project bool) error {
	var entries []entry
	var err error
	switch filepath.Ext(name) {
	case ".yaml", ".yml":
		entries, err = yamlEntries(data)
	case ".json":
		entries, err = jsonEntries(data)
	default:
		return fmt.Errorf("config: %s: unsupported format, use .yaml or .json", layer)
	}
	if err != nil {
		return fmt.Errorf("config: %s: %w", layer, err)
	}
	for _, e := range entries {
		where := layer
		if e.line > 0 {
			where += " line " + strconv.Itoa(e.line)
		}
		if e.err != nil {
			return fmt.Errorf("config: %s: %s: %w", where, e.key, e.err)
		}
		i := slices.IndexFunc(fs, func(f field) bool { return f.key == e.key })
		if i < 0 {
			return fmt.Errorf("config: %s: unknown key %q", where, e.key)
		}
		if fs[i].build() && !project {
			return fmt.Errorf("config: %s: build settings belong in the project tifa.yaml", where)
		}
		if want := fs[i].value.kind(); !accepts(want, e.typ) {
			return fmt.Errorf("config: %s: %s: expected a %s value, got %s %q", where, e.key, want, e.typ, e.raw)
		}
		if err := set(fs[i], where, e.raw); err != nil {
			return err
		}
	}
	return nil
}

// accepts reports whether a file value of type typ may set a setting of the given kind.
// Flags and environment variables are text and skip this check.
func accepts(kind, typ string) bool {
	switch kind {
	case "int":
		return typ == "number"
	case "size":
		return typ == "string" || typ == "number" // 1MiB or a plain byte count
	case "bool":
		return typ == "bool"
	case "pgo":
		return typ == "string" || typ == "bool" || typ == "number" // false or 0 fails with what to write
	default:
		return typ == "string"
	}
}

func yamlEntries(data []byte) ([]entry, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err == io.EOF {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("line %d: only one YAML document is allowed", extra.Line)
		}
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: expected a mapping of sections", root.Line)
	}
	var entries []entry
	for i := 0; i < len(root.Content); i += 2 {
		section, body := root.Content[i], root.Content[i+1]
		if !knownSection(section.Value) {
			entries = append(entries, entry{key: section.Value, line: section.Line})
			continue
		}
		if body.Kind != yaml.MappingNode {
			entries = append(entries, entry{key: section.Value, line: section.Line, err: errors.New("expected a mapping")})
			continue
		}
		for j := 0; j < len(body.Content); j += 2 {
			k, v := body.Content[j], body.Content[j+1]
			e := entry{key: section.Value + "." + k.Value, raw: v.Value, typ: yamlType(v.ShortTag()), line: k.Line}
			if v.Kind != yaml.ScalarNode {
				e.err = errors.New("expected a single value")
			}
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func jsonEntries(data []byte) ([]entry, error) {
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("only one JSON value is allowed")
	}
	var entries []entry
	for _, name := range sortedKeys(root) {
		section, ok := root[name].(map[string]any)
		if !knownSection(name) {
			entries = append(entries, entry{key: name})
			continue
		}
		if !ok {
			entries = append(entries, entry{key: name, err: errors.New("expected an object")})
			continue
		}
		for _, k := range sortedKeys(section) {
			e := entry{key: name + "." + k}
			switch v := section[k].(type) {
			case string:
				e.raw, e.typ = v, "string"
			case json.Number:
				e.raw, e.typ = v.String(), "number"
			case bool:
				e.raw, e.typ = strconv.FormatBool(v), "bool"
			case nil:
				e.raw, e.typ = "null", "null"
			default:
				e.err = fmt.Errorf("expected a single value, got %v", v)
			}
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func yamlType(tag string) string {
	switch tag {
	case "!!str":
		return "string"
	case "!!int", "!!float":
		return "number"
	case "!!bool":
		return "bool"
	case "!!null":
		return "null"
	}
	return tag
}

func knownSection(name string) bool {
	return slices.Contains([]string{"build", "server", "ssr", "cache"}, name)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Example renders the runtime settings of c as a commented deployment file.
func Example(c Config) []byte {
	var b strings.Builder
	b.WriteString("# tifa runtime settings. Precedence from high to low: flags, environment variables,\n")
	b.WriteString("# this file (--config), the tifa.yaml embedded at build time, defaults.\n")
	section := ""
	for _, f := range fields(&c) {
		if f.build() {
			continue
		}
		name, key, _ := strings.Cut(f.key, ".")
		if name != section {
			section = name
			fmt.Fprintf(&b, "%s:\n", name)
		}
		fmt.Fprintf(&b, "  # %s. Flag --%s, env %s.\n", f.help, f.flag(), f.env())
		value := f.value.String()
		if f.value.kind() == "string" {
			value = strconv.Quote(value)
		}
		fmt.Fprintf(&b, "  %s: %s\n", key, value)
	}
	return []byte(b.String())
}
