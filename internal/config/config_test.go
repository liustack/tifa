package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// load merges the settings of tifa from args, environ and a project tifa.yaml, empty for none,
// as tifa build and the program do between them. name is tifa.yaml or empty.
func load(t *testing.T, args, environ []string, name, project string) (Config, error) {
	t.Helper()
	build := Default().Build
	var settings map[string]string
	if name != "" {
		c, s, err := Project([]byte(project))
		if err != nil {
			return c, err
		}
		build, settings = c.Build, s
	}
	src := Read(args, environ, "")
	if err := src.Err(); err != nil {
		return Config{}, err
	}
	c, _, err := Load(src, settings)
	c.Build = build
	return c, err
}

// printed prints c with no settings of the app.
func printed(c Config) []byte { return Print(c, nil, nil) }

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLayersApplyInPrecedenceOrder(t *testing.T) {
	embedded := `
build:
  target: es2022
server:
  addr: 0.0.0.0:9000
  body_limit: 2MiB
ssr:
  vms: 3
  timeout: 4s
`
	file := writeFile(t, "prod.yaml", `
server:
  body_limit: 3MiB
ssr:
  vms: 5
  timeout: 5s
`)
	environ := []string{"PATH=/bin", "SSR_VMS=7", "SSR_TIMEOUT=6s"}
	c, err := load(t, []string{"--config", file, "--ssr-timeout", "7s"}, environ, "tifa.yaml", embedded)
	if err != nil {
		t.Fatal(err)
	}
	d := Default()
	checks := []struct {
		name      string
		got, want any
	}{
		{"default", c.Server.ShutdownTimeout, d.Server.ShutdownTimeout},
		{"default", c.Cache.Size, d.Cache.Size},
		{"embedded", c.Server.Addr, "0.0.0.0:9000"},
		{"embedded", c.Build.Target, "es2022"},
		{"file", c.Server.BodyLimit, int64(3 << 20)},
		{"env", c.SSR.VMs, 7},
		{"flag", c.SSR.Timeout, 7 * time.Second},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s layer: got %v, want %v", check.name, check.got, check.want)
		}
	}
}

// build.skip_tanstack_check is a true or false of the project file, off by default.
func TestSkipTanStackCheckIsABoolOfTheProjectFile(t *testing.T) {
	if Default().Build.SkipTanStackCheck {
		t.Fatal("the check is skipped by default")
	}
	for name, content := range map[string]string{
		"tifa.yaml": "build:\n  skip_tanstack_check: true\n",
	} {
		c, err := load(t, nil, nil, name, content)
		if err != nil || !c.Build.SkipTanStackCheck {
			t.Errorf("%s: %+v, %v", name, c.Build, err)
		}
	}
	if c, err := load(t, nil, nil, "tifa.yaml", "build:\n  skip_tanstack_check: false\n"); err != nil || c.Build.SkipTanStackCheck {
		t.Errorf("false: %+v, %v", c.Build, err)
	}
	for name, content := range map[string]string{
		"tifa.yaml": "build:\n  skip_tanstack_check: \"yes\"\n",
	} {
		if _, err := load(t, nil, nil, name, content); err == nil || !strings.Contains(err.Error(), "build.skip_tanstack_check") || !strings.Contains(err.Error(), "expected a bool value") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	// A deployment file has no build settings: they are left alone like any other key.
	if _, err := load(t, []string{"--config", writeFile(t, "prod.yaml", "build:\n  skip_tanstack_check: true\n")}, nil, "", ""); err != nil {
		t.Errorf("in a deployment file: %v", err)
	}
}

// build.debug_info is a true or false of the project file, off by default.
func TestDebugInfoIsABoolOfTheProjectFile(t *testing.T) {
	if Default().Build.DebugInfo {
		t.Fatal("debug info is kept by default")
	}
	for name, content := range map[string]string{
		"tifa.yaml": "build:\n  debug_info: true\n",
	} {
		c, err := load(t, nil, nil, name, content)
		if err != nil || !c.Build.DebugInfo {
			t.Errorf("%s: %+v, %v", name, c.Build, err)
		}
	}
	if _, err := load(t, nil, nil, "tifa.yaml", "build:\n  debug_info: \"yes\"\n"); err == nil || !strings.Contains(err.Error(), "build.debug_info") || !strings.Contains(err.Error(), "expected a bool value") {
		t.Errorf("a string: %v", err)
	}
	// A deployment file has no build settings: they are left alone like any other key.
	if _, err := load(t, []string{"--config", writeFile(t, "prod.yaml", "build:\n  debug_info: true\n")}, nil, "", ""); err != nil {
		t.Errorf("in a deployment file: %v", err)
	}
}

// build.i18next_cache is a true or false of the project file, on by default.
func TestI18nextCacheIsABoolOfTheProjectFile(t *testing.T) {
	if !Default().Build.I18nextCache {
		t.Fatal("the i18next cache is off by default")
	}
	for name, content := range map[string]string{
		"tifa.yaml": "build:\n  i18next_cache: false\n",
	} {
		c, err := load(t, nil, nil, name, content)
		if err != nil || c.Build.I18nextCache {
			t.Errorf("%s: %+v, %v", name, c.Build, err)
		}
	}
	if _, err := load(t, nil, nil, "tifa.yaml", "build:\n  i18next_cache: \"yes\"\n"); err == nil || !strings.Contains(err.Error(), "build.i18next_cache") || !strings.Contains(err.Error(), "expected a bool value") {
		t.Errorf("a string: %v", err)
	}
	// A deployment file has no build settings: they are left alone like any other key.
	if _, err := load(t, []string{"--config", writeFile(t, "prod.yaml", "build:\n  i18next_cache: false\n")}, nil, "", ""); err != nil {
		t.Errorf("in a deployment file: %v", err)
	}
}

func TestNoFilesGivesDefaults(t *testing.T) {
	c, err := load(t, nil, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Fatalf("%+v", c)
	}
}

func TestErrorsNameLayerAndKey(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		environ  []string
		embedded string
		file     string
		want     []string
	}{
		{name: "flag type", args: []string{"--ssr-vms", "two"}, want: []string{"flag --ssr-vms", "ssr.vms", `"two"`}},
		{name: "flag unit", args: []string{"--server-body-limit", "1MB"}, want: []string{"flag --server-body-limit", "server.body_limit", `"1MB"`}},
		{name: "env type", environ: []string{"SERVER_SHUTDOWN_TIMEOUT=soon"}, want: []string{"env SERVER_SHUTDOWN_TIMEOUT", "server.shutdown_timeout"}},
		{name: "file unit", file: "cache:\n  size: 64MB\n", want: []string{"file prod.yaml line 2", "cache.size", `"64MB"`}},
		{name: "file range", file: "ssr:\n  vms: 0\n", want: []string{"file prod.yaml line 2", "ssr.vms"}},
		{name: "file list", file: "server:\n  addr: [a, b]\n", want: []string{"file prod.yaml line 2", "server.addr"}},
		{name: "embedded type", embedded: "ssr:\n  timeout: 3\n", want: []string{"tifa.yaml line 2", "ssr.timeout"}},
		{name: "embedded syntax", embedded: "ssr: [\n", want: []string{"tifa.yaml"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.args
			if tc.file != "" {
				args = append(args, "--config", writeFile(t, "prod.yaml", tc.file))
			}
			_, err := load(t, args, tc.environ, "tifa.yaml", tc.embedded)
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestExampleLoadsBackToSameValues(t *testing.T) {
	c := Default()
	c.Server.Addr = "0.0.0.0:80"
	c.Cache.Size = 3 << 30
	c.SSR.VMMemory = 1536 << 10
	example := printed(c)
	for _, want := range []string{"--server-addr", "SERVER_ADDR", "server:", "body_limit: 1MiB"} {
		if !strings.Contains(string(example), want) {
			t.Errorf("example lacks %q:\n%s", want, example)
		}
	}
	if strings.Contains(string(example), "build:") {
		t.Error("example must only hold runtime settings")
	}
	got, err := load(t, []string{"--config", writeFile(t, "example.yaml", string(example))}, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip changed values:\n%+v\n%+v", got, c)
	}
}

func TestFileValuesMustHaveTheRightType(t *testing.T) {
	cases := []struct{ name, file, content, key string }{
		{"yaml bool addr", "prod.yaml", "server:\n  addr: false\n", "server.addr"},
		{"yaml null addr", "prod.yaml", "server:\n  addr: null\n", "server.addr"},
		{"yaml empty addr", "prod.yaml", "server:\n  addr:\n", "server.addr"},
		{"yaml number addr", "prod.yaml", "server:\n  addr: 9527\n", "server.addr"},
		{"yaml quoted vms", "prod.yaml", "ssr:\n  vms: \"2\"\n", "ssr.vms"},
		{"yaml bool size", "prod.yaml", "cache:\n  size: true\n", "cache.size"},
		{"yaml number timeout", "prod.yaml", "ssr:\n  timeout: 3\n", "ssr.timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, []string{"--config", writeFile(t, tc.file, tc.content)}, nil, "", "")
			if err == nil || !strings.Contains(err.Error(), "file "+tc.file) || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("got %v", err)
			}
		})
	}
	ok := []string{"server:\n  body_limit: 1048576\n", "server:\n  body_limit: \"1MiB\"\n", "ssr:\n  vms: 2\n"}
	for _, content := range ok {
		if _, err := load(t, []string{"--config", writeFile(t, "ok.yaml", content)}, nil, "", ""); err != nil {
			t.Errorf("%q: %v", content, err)
		}
	}
}

func TestFileMustHoldOneDocument(t *testing.T) {
	cases := map[string]string{
		"prod.yaml": "server:\n  addr: :1\n---\nssr:\n  vms: 0\n",
	}
	for name, content := range cases {
		_, err := load(t, []string{"--config", writeFile(t, name, content)}, nil, "", "")
		if err == nil || !strings.Contains(err.Error(), "file "+name) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	for _, content := range []string{"", "# only a comment\n", "---\nserver:\n  addr: :1\n"} {
		if _, err := load(t, []string{"--config", writeFile(t, "one.yaml", content)}, nil, "", ""); err != nil {
			t.Errorf("%q: %v", content, err)
		}
	}
}

func TestKeysListEverySetting(t *testing.T) {
	want := "build.target build.public build.skip_tanstack_check build.debug_info build.i18next_cache server.addr server.body_limit server.shutdown_timeout server.compress server.tls_domains server.tls_email server.tls_cache server.tls_ca server.tls_cert server.tls_key server.http_addr server.hsts server.trusted_origins server.cross_site_paths ssr.vms ssr.vm_memory ssr.vm_stack_depth ssr.timeout ssr.queue_limit ssr.wait_timeout cache.size cache.ignore_query session.cookie session.max_age session.memory_limit"
	if got := strings.Join(Keys(), " "); got != want {
		t.Fatalf("Keys: %s, want %s", got, want)
	}
}

// server.compress is on by default and turns off from every runtime source.
func TestCompressIsOnUnlessTurnedOff(t *testing.T) {
	if !Default().Server.Compress {
		t.Fatal("compression is off by default")
	}
	for name, tc := range map[string]struct {
		args, environ []string
	}{
		"flag": {args: []string{"--server-compress=false"}},
		"env":  {environ: []string{"SERVER_COMPRESS=false"}},
		"file": {args: []string{"--config", writeFile(t, "prod.yaml", "server:\n  compress: false\n")}},
	} {
		c, err := load(t, tc.args, tc.environ, "", "")
		if err != nil || c.Server.Compress {
			t.Errorf("%s: compress %v, %v", name, c.Server.Compress, err)
		}
	}
	if _, err := load(t, nil, []string{"SERVER_COMPRESS=gzip"}, "", ""); err == nil || !strings.Contains(err.Error(), "server.compress") {
		t.Errorf("a word that is not a bool: %v", err)
	}
}

// tifa dev and a built app both listen on 9527 unless told otherwise.
func TestDefaultAddr(t *testing.T) {
	if DefaultAddr != "127.0.0.1:9527" || Default().Server.Addr != DefaultAddr {
		t.Fatalf("DefaultAddr = %q, Default().Server.Addr = %q", DefaultAddr, Default().Server.Addr)
	}
}

// ssr.vm_stack_depth is a whole number from 1 to 10000, 10000 by default: the deepest the
// engine nests calls, which internal/ssr checks.
func TestVMStackDepth(t *testing.T) {
	if got := Default().SSR.VMStackDepth; got != 10000 {
		t.Fatalf("default %d", got)
	}
	c, err := load(t, nil, nil, "tifa.yaml", "ssr:\n  vm_stack_depth: 1000\n")
	if err != nil || c.SSR.VMStackDepth != 1000 {
		t.Fatalf("file: %d, %v", c.SSR.VMStackDepth, err)
	}
	c, err = load(t, []string{"--ssr-vm-stack-depth", "10000"}, nil, "", "")
	if err != nil || c.SSR.VMStackDepth != 10000 {
		t.Fatalf("10000: %d, %v", c.SSR.VMStackDepth, err)
	}
	for _, tc := range []struct {
		name, layer   string
		args, environ []string
		file          string
	}{
		{"flag", "flag --ssr-vm-stack-depth", []string{"--ssr-vm-stack-depth", "10001"}, nil, ""},
		{"env", "env SSR_VM_STACK_DEPTH", nil, []string{"SSR_VM_STACK_DEPTH=10001"}, ""},
		{"file", "file prod.yaml", nil, nil, "ssr:\n  vm_stack_depth: 100000\n"},
	} {
		args := tc.args
		if tc.file != "" {
			args = []string{"--config", writeFile(t, "prod.yaml", tc.file)}
		}
		_, err := load(t, args, tc.environ, "", "")
		if err == nil {
			t.Errorf("%s: accepted", tc.name)
			continue
		}
		for _, want := range []string{tc.layer, "ssr.vm_stack_depth", "above the maximum 10000", "the engine does not nest calls deeper"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %v lacks %q", tc.name, err, want)
			}
		}
	}
	_, err = load(t, nil, nil, "tifa.yaml", "ssr:\n  vm_stack_depth: 20000\n")
	if err == nil || !strings.Contains(err.Error(), "tifa.yaml line 2") || !strings.Contains(err.Error(), "above the maximum 10000") {
		t.Errorf("project file: %v", err)
	}
	c, err = load(t, []string{"--ssr-vm-stack-depth", "3000"}, []string{"SSR_VM_STACK_DEPTH=2000"}, "tifa.yaml", "ssr:\n  vm_stack_depth: 1000\n")
	if err != nil || c.SSR.VMStackDepth != 3000 {
		t.Fatalf("flag: %d, %v", c.SSR.VMStackDepth, err)
	}
	c, err = load(t, nil, []string{"SSR_VM_STACK_DEPTH=2000"}, "tifa.yaml", "ssr:\n  vm_stack_depth: 1000\n")
	if err != nil || c.SSR.VMStackDepth != 2000 {
		t.Fatalf("env: %d, %v", c.SSR.VMStackDepth, err)
	}
	_, err = load(t, nil, []string{"SSR_VM_STACK_DEPTH=0"}, "", "")
	if err == nil || !strings.Contains(err.Error(), "ssr.vm_stack_depth") || !strings.Contains(err.Error(), "below the minimum 1") {
		t.Fatalf("0: %v", err)
	}
}

// HTTPS is off unless server.tls_domains or server.tls_cert is set, so a program behind a proxy
// or CDN serves plain HTTP on server.addr as before.
func TestHTTPSIsOffByDefault(t *testing.T) {
	s := Default().Server
	if s.HTTPS() || s.TLSDomains != nil || s.TLSCert != "" || s.TLSKey != "" || s.TLSEmail != "" || s.TLSCache != "" || s.HSTS {
		t.Fatalf("defaults turn HTTPS on: %+v", s)
	}
	if s.TLSCA != "https://acme-v02.api.letsencrypt.org/directory" || s.HTTPAddr != ":80" {
		t.Fatalf("tls_ca %q, http_addr %q", s.TLSCA, s.HTTPAddr)
	}
	if err := s.CheckHTTPS(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPSSettingsComeFromEveryLayer(t *testing.T) {
	c, err := load(t, nil, []string{"SERVER_TLS_DOMAINS=example.com, www.Example.com"}, "tifa.yaml",
		"server:\n  addr: :443\n  tls_cache: /var/lib/app/certs\n  tls_email: ops@example.com\n  http_addr: off\n  hsts: true\n")
	if err != nil {
		t.Fatal(err)
	}
	s := c.Server
	if !reflect.DeepEqual(s.TLSDomains, []string{"example.com", "www.example.com"}) || s.TLSCache != "/var/lib/app/certs" ||
		s.TLSEmail != "ops@example.com" || s.HTTPAddr != "off" || !s.HSTS || !s.HTTPS() {
		t.Fatalf("%+v", s)
	}
	if err := s.CheckHTTPS(); err != nil {
		t.Fatal(err)
	}
	c, err = load(t, []string{"--server-addr", ":443", "--server-tls-cert", "cert.pem", "--server-tls-key", "key.pem"}, nil, "", "")
	if err != nil || c.Server.TLSCert != "cert.pem" || c.Server.TLSKey != "key.pem" || !c.Server.HTTPS() || c.Server.CheckHTTPS() != nil {
		t.Fatalf("own files: %+v, %v", c.Server, err)
	}
	// An empty value from a higher layer turns off what a lower one set, so a deployment can
	// use its own files where the project file asks for Let's Encrypt.
	c, err = load(t, []string{"--server-addr", ":443", "--server-tls-cert", "cert.pem", "--server-tls-key", "key.pem"},
		[]string{"SERVER_TLS_DOMAINS=", "SERVER_TLS_CACHE="}, "tifa.yaml", "server:\n  tls_domains: example.com\n  tls_cache: certs\n")
	if err != nil || c.Server.TLSDomains != nil || c.Server.CheckHTTPS() != nil {
		t.Fatalf("unset domains: %+v, %v", c.Server, err)
	}
}

func TestHTTPSSettingsThatDoNotGoTogetherFail(t *testing.T) {
	for _, tc := range []struct {
		name  string
		yaml  string
		wants []string
	}{
		{"both kinds", "tls_domains: example.com\n  tls_cache: certs\n  tls_cert: c.pem\n  tls_key: k.pem\n", []string{"server.tls_domains", "server.tls_cert", "only one"}},
		{"cert alone", "tls_cert: c.pem\n", []string{"server.tls_cert", "server.tls_key"}},
		{"key alone", "tls_key: k.pem\n", []string{"server.tls_key", "server.tls_cert"}},
		{"no cache", "tls_domains: example.com\n", []string{"server.tls_domains", "server.tls_cache", "restarts"}},
		{"email alone", "tls_email: ops@example.com\n", []string{"server.tls_email", "server.tls_domains"}},
		{"cache alone", "tls_cache: certs\n", []string{"server.tls_cache", "server.tls_domains"}},
		{"cache with own files", "tls_cert: c.pem\n  tls_key: k.pem\n  tls_cache: certs\n", []string{"server.tls_cache", "server.tls_domains"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A project file may leave out what the deployment adds, so it loads and only
			// CheckHTTPS, which serve calls, fails.
			c, err := load(t, nil, nil, "tifa.yaml", "server:\n  "+tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			err = c.Server.CheckHTTPS()
			if err == nil {
				t.Fatal("accepted")
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%v lacks %q", err, want)
				}
			}
		})
	}
}

func TestBadHTTPSValuesFailWithTheirLayer(t *testing.T) {
	for _, tc := range []struct{ env, want string }{
		{"SERVER_TLS_DOMAINS=*.example.com", "wildcard"},
		{"SERVER_TLS_DOMAINS=https://example.com", "scheme"},
		{"SERVER_TLS_DOMAINS=example.com:443", "port"},
		{"SERVER_TLS_DOMAINS=203.0.113.7", "IP address"},
		{"SERVER_TLS_DOMAINS=203.0.113.7.", "IP address"},
		{"SERVER_TLS_DOMAINS=localhost", "dot"},
		{"SERVER_TLS_DOMAINS=example.com,,www.example.com", "empty"},
		{"SERVER_TLS_CA=http://acme.example.com/directory", "https"},
		{"SERVER_HSTS=yes please", "server.hsts"},
	} {
		name, _, _ := strings.Cut(tc.env, "=")
		_, err := load(t, nil, []string{tc.env}, "", "")
		if err == nil || !strings.Contains(err.Error(), "env "+name) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.env, err)
		}
	}
}

func TestExampleHoldsTheHTTPSSettings(t *testing.T) {
	c := Default()
	c.Server.TLSDomains = []string{"example.com", "www.example.com"}
	c.Server.TLSCache = "/var/lib/app/certs"
	c.Server.HTTPAddr = "off"
	example := printed(c)
	for _, want := range []string{`tls_domains: "example.com,www.example.com"`, "SERVER_TLS_CERT", `tls_cert: ""`, `http_addr: "off"`, "hsts: false"} {
		if !strings.Contains(string(example), want) {
			t.Errorf("example lacks %q:\n%s", want, example)
		}
	}
	got, err := load(t, []string{"--config", writeFile(t, "example.yaml", string(example))}, nil, "", "")
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip: %+v, %v", got.Server, err)
	}
}

// Domains are stored the way autocert compares them, in ASCII without a final dot: a name
// HostWhitelist cannot convert would otherwise be dropped without a word, and a final dot would
// never match the name a browser sends.
func TestTLSDomainsAreStoredAsASCII(t *testing.T) {
	c, err := load(t, nil, []string{"SERVER_TLS_DOMAINS=Example.com., bücher.example, www.example.com"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"example.com", "xn--bcher-kva.example", "www.example.com"}; !reflect.DeepEqual(c.Server.TLSDomains, want) {
		t.Fatalf("%q, want %q", c.Server.TLSDomains, want)
	}
	for _, bad := range []string{"a_b.example.com", "-x.example.com", "exa mple.com", "example..com", "example.com.."} {
		_, err := load(t, nil, []string{"SERVER_TLS_DOMAINS=" + bad}, "", "")
		if err == nil || !strings.Contains(err.Error(), "server.tls_domains") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// server.http_addr is an address or off, in lower case like the other words of the settings.
func TestHTTPAddrIsAnAddressOrOff(t *testing.T) {
	for _, good := range []string{":80", "0.0.0.0:80", "[::]:8080", "off"} {
		c, err := load(t, nil, []string{"SERVER_HTTP_ADDR=" + good}, "", "")
		if err != nil || c.Server.HTTPAddr != good {
			t.Errorf("%q: %q, %v", good, c.Server.HTTPAddr, err)
		}
	}
	for _, tc := range []struct{ value, want string }{
		{"OFF", "write off"}, {"false", "write off"}, {"none", "write off"}, {"80", "such as :80"}, {"example.com", "such as :80"},
	} {
		_, err := load(t, nil, []string{"SERVER_HTTP_ADDR=" + tc.value}, "", "")
		if err == nil || !strings.Contains(err.Error(), "server.http_addr") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: %v", tc.value, err)
		}
	}
}

// With HTTPS on, server.addr must be set: the default only listens on the loopback, where no
// browser and no Let's Encrypt reaches it. An address set by hand, loopback or not, is kept.
func TestHTTPSNeedsServerAddr(t *testing.T) {
	c, err := load(t, nil, []string{"SERVER_TLS_CERT=c.pem", "SERVER_TLS_KEY=k.pem"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Server.CheckHTTPS()
	if err == nil || !strings.Contains(err.Error(), "server.addr is 127.0.0.1:9527, the default") || !strings.Contains(err.Error(), ":443") {
		t.Fatalf("default addr: %v", err)
	}
	// Written out by hand it is the same address, and the error reads right for it too.
	c, err = load(t, []string{"--server-addr", "127.0.0.1:9527"}, []string{"SERVER_TLS_CERT=c.pem", "SERVER_TLS_KEY=k.pem"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Server.CheckHTTPS(); err == nil || !strings.Contains(err.Error(), "server.addr is 127.0.0.1:9527, the default") {
		t.Fatalf("default addr written out: %v", err)
	}
	c.Server.Addr = "127.0.0.1:8443"
	if err := c.Server.CheckHTTPS(); err != nil {
		t.Fatalf("loopback set by hand: %v", err)
	}
}

func TestSSRQueueSettings(t *testing.T) {
	c, err := load(t, nil, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.SSR.QueueLimit != 32 || c.SSR.WaitTimeout != time.Second {
		t.Fatalf("defaults: %+v", c.SSR)
	}
	file := writeFile(t, "deploy.yaml", "ssr:\n  queue_limit: 8\n  wait_timeout: 800ms\n")
	c, err = load(t, []string{"--config", file, "--ssr-queue-limit", "0", "--ssr-wait-timeout", "200ms"}, []string{"SSR_QUEUE_LIMIT=4", "SSR_WAIT_TIMEOUT=400ms"}, "tifa.yaml", "ssr:\n  queue_limit: 16\n  wait_timeout: 2s\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.SSR.QueueLimit != 0 || c.SSR.WaitTimeout != 200*time.Millisecond {
		t.Fatalf("flags: %+v", c.SSR)
	}
	for _, setting := range []string{"queue_limit: -1", "queue_limit: 0.5", "wait_timeout: 0s", "wait_timeout: -1s", "wait_timeout: 1"} {
		t.Run(setting, func(t *testing.T) {
			if _, err := load(t, nil, nil, "tifa.yaml", "ssr:\n  "+setting+"\n"); err == nil {
				t.Fatal("accepted invalid queue setting")
			}
		})
	}
}

// Cross-site writes are refused unless the origin is trusted or the path is listed. Neither list is
// set by default, and a higher layer can empty what a lower one set.
func TestCrossSiteSettingsComeFromEveryLayer(t *testing.T) {
	c, err := load(t, nil, nil, "", "")
	if err != nil || c.Server.TrustedOrigins != nil || c.Server.CrossSitePaths != nil {
		t.Fatalf("defaults: %+v, %v", c.Server, err)
	}
	c, err = load(t, []string{"--server-cross-site-paths", "POST /auth/apple/callback"},
		[]string{"SERVER_TRUSTED_ORIGINS=https://admin.example.com, http://localhost:3000"},
		"tifa.yaml", "server:\n  trusted_origins: https://old.example.com\n  cross_site_paths: /hooks/{name}, GET /x\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://admin.example.com", "http://localhost:3000"}; !reflect.DeepEqual(c.Server.TrustedOrigins, want) {
		t.Errorf("trusted origins %q, want %q", c.Server.TrustedOrigins, want)
	}
	if want := []string{"POST /auth/apple/callback"}; !reflect.DeepEqual(c.Server.CrossSitePaths, want) {
		t.Errorf("cross-site paths %q, want %q", c.Server.CrossSitePaths, want)
	}
	c, err = load(t, nil, []string{"SERVER_TRUSTED_ORIGINS=", "SERVER_CROSS_SITE_PATHS="},
		"tifa.yaml", "server:\n  trusted_origins: https://old.example.com\n  cross_site_paths: /hooks/{name}\n")
	if err != nil || c.Server.TrustedOrigins != nil || c.Server.CrossSitePaths != nil {
		t.Fatalf("emptied: %+v, %v", c.Server, err)
	}
}

// An origin is a scheme, a host and maybe a port, written the way a browser sends it: lower case
// and without the default port of its scheme, or no request would ever match it. A host that a
// browser writes another way, such as one with letters outside ASCII or an IP address not in its
// shortest form, fails and names the form to write, since the server compares the text.
func TestTrustedOriginsAreWrittenTheWayABrowserSendsThem(t *testing.T) {
	c, err := load(t, nil, []string{"SERVER_TRUSTED_ORIGINS=HTTPS://Admin.Example.com:443, http://localhost:3000, http://a.test:80, https://[2001:DB8::1]:8443, http://192.0.2.1, http://example.0xg"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://admin.example.com", "http://localhost:3000", "http://a.test", "https://[2001:db8::1]:8443", "http://192.0.2.1", "http://example.0xg"}; !reflect.DeepEqual(c.Server.TrustedOrigins, want) {
		t.Fatalf("%q, want %q", c.Server.TrustedOrigins, want)
	}
	for _, tc := range []struct{ value, want string }{
		{"admin.example.com", "scheme"},
		{"https://", "host"},
		{"https://example.com/", "path"},
		{"https://example.com/app", "path"},
		{"https://example.com?x=1", "query"},
		{"ftp://example.com", "http or https"},
		{"https://a.test,,https://b.test", "empty"},
		{"*", "scheme"},
		{"https://İ.example", "xn--i-9bb.example"},
		{"https://bücher.example", "xn--bcher-kva.example"},
		{"https://[2001:0DB8:0:0:0:0:0:1]", "[2001:db8::1]"},
		{"https://admin.example:0443", "leading zeros"},
		{"http://010.0.0.1", "four decimal"},
		{"http://0x7f.0.0.1", "four decimal"},
		{"http://127.1", "four decimal"},
		{"http://1.2.3.256", "four decimal"},
		{"https://example.com!", "only letters"},
		{"https://[::ffff:192.0.2.1]", "IPv4"},
		{"http://1..2", "empty part"},
		{"http://example.com..", "empty part"},
		{"https://a.test:99999", "port"},
	} {
		_, err := load(t, nil, []string{"SERVER_TRUSTED_ORIGINS=" + tc.value}, "", "")
		if err == nil || !strings.Contains(err.Error(), "server.trusted_origins") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: %v, want a message with %q", tc.value, err, tc.want)
		}
	}
}

// A path pattern that http.ServeMux would panic on fails at load, with the layer and the key.
func TestCrossSitePathsAreServeMuxPatterns(t *testing.T) {
	for _, good := range []string{"/auth/callback", "POST /auth/callback", "/hooks/{name}", "/files/{path...}", "/a, POST /b"} {
		if _, err := load(t, nil, []string{"SERVER_CROSS_SITE_PATHS=" + good}, "", ""); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"POST", "/a/{", "/a/{b}/{b}", "/a,,/b", "/a, /a"} {
		_, err := load(t, nil, []string{"SERVER_CROSS_SITE_PATHS=" + bad}, "", "")
		if err == nil || !strings.Contains(err.Error(), "server.cross_site_paths") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestExampleHoldsTheCrossSiteSettings(t *testing.T) {
	c := Default()
	c.Server.TrustedOrigins = []string{"https://admin.example.com", "http://localhost:3000"}
	out := string(printed(c))
	for _, want := range []string{`trusted_origins: "https://admin.example.com,http://localhost:3000"`, "SERVER_CROSS_SITE_PATHS", `cross_site_paths: ""`} {
		if !strings.Contains(out, want) {
			t.Errorf("example lacks %s:\n%s", want, out)
		}
	}
	back, err := load(t, nil, nil, "x.yaml", out)
	if err != nil || !reflect.DeepEqual(back.Server.TrustedOrigins, c.Server.TrustedOrigins) {
		t.Fatalf("round trip: %+v, %v", back.Server, err)
	}
}

func TestSessionSettings(t *testing.T) {
	d := Default().Session
	if d.Cookie != "__Host-tifa_session" || d.MaxAge != 720*time.Hour || d.MemoryLimit != 64<<20 {
		t.Fatalf("defaults %+v", d)
	}
	c, err := load(t, []string{"--session-cookie", "app_sid"}, []string{"SESSION_MAX_AGE=24h"}, "tifa.yaml", "session:\n  memory_limit: 8MiB\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Session.Cookie != "app_sid" || c.Session.MaxAge != 24*time.Hour || c.Session.MemoryLimit != 8<<20 {
		t.Fatalf("%+v", c.Session)
	}
}

func TestSessionSettingErrors(t *testing.T) {
	cases := []struct {
		name, file string
		want       []string
	}{
		{"cookie name", "session:\n  cookie: \"a b\"\n", []string{"session.cookie", "cookie name"}},
		{"cookie semicolon", "session:\n  cookie: \"a;b\"\n", []string{"session.cookie"}},
		{"cookie empty", "session:\n  cookie: \"\"\n", []string{"session.cookie"}},
		{"max age zero", "session:\n  max_age: 0s\n", []string{"session.max_age"}},
		{"max age under a second", "session:\n  max_age: 500ms\n", []string{"session.max_age"}},
		{"memory limit unit", "session:\n  memory_limit: 8MB\n", []string{"session.memory_limit"}},
		{"memory limit zero", "session:\n  memory_limit: 0\n", []string{"session.memory_limit"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, []string{"--config", writeFile(t, "prod.yaml", tc.file)}, nil, "", "")
			if err == nil {
				t.Fatal("no error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("%v lacks %q", err, w)
				}
			}
		})
	}
}

// cache.ignore_query starts as the list of tracking parameters, and each setting adds names to it
// or takes them out, so a layer needs to say only what it changes.
func TestCacheIgnoreQueryAddsAndRemoves(t *testing.T) {
	defaults := Default().Cache.IgnoreQuery
	for _, name := range []string{"utm_*", "gclid", "fbclid", "msclkid", "twclid", "ttclid", "yclid", "gbraid", "wbraid", "mc_cid", "mc_eid"} {
		if !slices.Contains(defaults, name) {
			t.Errorf("the default list lacks %s: %v", name, defaults)
		}
	}
	cases := map[string]struct {
		args, environ []string
		want          []string // names that must be in the list
		gone          []string // names that must not
	}{
		"add":        {args: []string{"--cache-ignore-query", "ref, _hsenc"}, want: []string{"ref", "_hsenc", "gclid"}},
		"remove":     {args: []string{"--cache-ignore-query=-gclid,-utm_*"}, want: []string{"fbclid"}, gone: []string{"gclid", "utm_*"}},
		"env":        {environ: []string{"CACHE_IGNORE_QUERY=ref,-yclid"}, want: []string{"ref"}, gone: []string{"yclid"}},
		"remove all": {args: []string{"--cache-ignore-query=-*,ref"}, want: []string{"ref"}, gone: []string{"gclid", "utm_*"}},
		"file":       {args: []string{"--config", writeFile(t, "prod.yaml", "cache:\n  ignore_query: ref\n")}, want: []string{"ref", "gclid"}},
		"twice":      {args: []string{"--config", writeFile(t, "prod.yaml", "cache:\n  ignore_query: ref\n"), "--cache-ignore-query=-ref"}, gone: []string{"ref"}, want: []string{"gclid"}},
	}
	for name, tc := range cases {
		c, err := load(t, tc.args, tc.environ, "", "")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		for _, w := range tc.want {
			if !slices.Contains(c.Cache.IgnoreQuery, w) {
				t.Errorf("%s: %v lacks %s", name, c.Cache.IgnoreQuery, w)
			}
		}
		for _, g := range tc.gone {
			if slices.Contains(c.Cache.IgnoreQuery, g) {
				t.Errorf("%s: %v still has %s", name, c.Cache.IgnoreQuery, g)
			}
		}
	}
	// Loading does not change the defaults for the next load.
	if !slices.Equal(Default().Cache.IgnoreQuery, defaults) {
		t.Error("a load changed the default list")
	}
}

func TestCacheIgnoreQueryRefusesBadNames(t *testing.T) {
	for _, bad := range []string{"a=b", "a&b", "*x", "a*b", "a b", "?a", "a,,b"} {
		_, err := load(t, []string{"--cache-ignore-query", bad}, nil, "", "")
		if err == nil || !strings.Contains(err.Error(), "cache.ignore_query") {
			t.Errorf("%q: got %v", bad, err)
		}
	}
}
