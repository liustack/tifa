package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Only the keys of tifa are read: other keys of files, variables and flags are left alone,
// the prefix TIFA_ included.
func TestUnknownNamesAreLeftAlone(t *testing.T) {
	file := writeFile(t, "prod.yaml", "sever:\n  addr: :1\nserver:\n  adr: :1\n  vms: x\ndb:\n  url: app.db\n")
	environ := []string{"SERVER_ADR=:1", "TIFA_SSR_VMS=nope", "TIFA_E2E=1", "DB_URL=x"}
	c, err := load(t, []string{"serve", "-test.v", "--server-adr=:1", "--db-url", "x", "-c", file}, environ, "tifa.yaml", "server:\n  port: 80\nshop:\n  open: true\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.SSR.VMs != Default().SSR.VMs || c.Server.Addr != DefaultAddr {
		t.Fatalf("%+v", c)
	}
}

func TestVariablesHaveNoPrefix(t *testing.T) {
	c, err := load(t, nil, []string{"SSR_VMS=7", "SERVER_BODY_LIMIT=2MiB"}, "", "")
	if err != nil || c.SSR.VMs != 7 || c.Server.BodyLimit != 2<<20 {
		t.Fatalf("%+v, %v", c.SSR, err)
	}
}

func TestConfigFlagHasAShortForm(t *testing.T) {
	file := writeFile(t, "prod.yaml", "ssr:\n  vms: 3\n")
	for _, args := range [][]string{{"-c", file}, {"-c=" + file}, {"--config", file}, {"--config=" + file}, {"serve", "-c", file}} {
		c, err := load(t, args, nil, "", "")
		if err != nil || c.SSR.VMs != 3 {
			t.Errorf("%q: vms %d, %v", args, c.SSR.VMs, err)
		}
	}
	if _, err := load(t, []string{"-c", filepath.Join(t.TempDir(), "missing.yaml")}, nil, "", ""); err == nil {
		t.Error("a missing file of -c must fail")
	}
	if _, err := load(t, []string{"-c"}, nil, "", ""); err == nil {
		t.Error("-c without a file must fail")
	}
	if _, err := load(t, []string{"--", "-c", "nothing.yaml"}, nil, "", ""); err != nil {
		t.Errorf("after --: %v", err)
	}
}

func TestBoolFlagTakesNoNextWord(t *testing.T) {
	src := Read([]string{"--server-hsts", "serve", "--server-compress=false"}, nil, "")
	c, _, err := Load(src, nil)
	if err != nil || !c.Server.HSTS || c.Server.Compress {
		t.Fatalf("hsts %v compress %v, %v", c.Server.HSTS, c.Server.Compress, err)
	}
	if _, _, err := Load(Read([]string{"--ssr-vms"}, nil, ""), nil); err == nil || !strings.Contains(err.Error(), "--ssr-vms needs a value") {
		t.Fatalf("a flag without its value: %v", err)
	}
}

func dotenvDir(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDotenvSitsBetweenTheEnvironmentAndTheFile(t *testing.T) {
	dir := dotenvDir(t, "SSR_VMS=5\nSSR_TIMEOUT=4s\nCACHE_SIZE=1MiB\n")
	file := writeFile(t, "prod.yaml", "ssr:\n  vms: 2\n  timeout: 2s\n  queue_limit: 9\n")
	src := Read([]string{"-c", file}, []string{"SSR_TIMEOUT=6s"}, dir)
	if err := src.Err(); err != nil {
		t.Fatal(err)
	}
	c, from, err := Load(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.SSR.VMs != 5 || c.SSR.Timeout != 6*time.Second || c.SSR.QueueLimit != 9 || c.Cache.Size != 1<<20 {
		t.Fatalf("%+v %+v", c.SSR, c.Cache)
	}
	want := map[string]string{"ssr.vms": ".env SSR_VMS", "ssr.timeout": "env SSR_TIMEOUT", "ssr.queue_limit": "file prod.yaml", "cache.size": ".env CACHE_SIZE"}
	for k, v := range want {
		if from[k] != v {
			t.Errorf("%s from %q, want %q", k, from[k], v)
		}
	}
	if _, ok := from["server.addr"]; ok {
		t.Errorf("a default has a source: %q", from["server.addr"])
	}
	// No .env is no error.
	if err := Read(nil, nil, t.TempDir()).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestDotenvSyntax(t *testing.T) {
	vars, err := parseDotenv([]byte(`# comment

A=plain value # comment
export B=1
C="quoted # not a comment \"x\" \n end" # comment
D='single \n $x'
E=
F = spaced
G=a#b
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "plain value", "B": "1", "C": "quoted # not a comment \"x\" \n end", "D": `single \n $x`, "E": "", "F": "spaced", "G": "a#b"}
	for k, v := range want {
		if vars[k] != v {
			t.Errorf("%s = %q, want %q", k, vars[k], v)
		}
	}
	for _, bad := range []string{"just words\n", "A=1\nB=\"open\n", "C='open\n", "1A=x\n", "D=\"x\" y\n"} {
		if _, err := parseDotenv([]byte(bad)); err == nil || !strings.Contains(err.Error(), "line") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	src := Read(nil, nil, dotenvDir(t, "OK=1\nbroken line\n"))
	if err := src.Err(); err == nil || !strings.Contains(err.Error(), ".env line 2") {
		t.Errorf("a broken .env: %v", err)
	}
}

func TestPortAndHostOfPlatforms(t *testing.T) {
	cases := []struct {
		name          string
		args, environ []string
		dotenv        string
		project       string
		want, from    string
	}{
		{"port", nil, []string{"PORT=8080"}, "", "", ":8080", "env PORT"},
		{"host", nil, []string{"HOST=0.0.0.0"}, "", "server:\n  addr: 127.0.0.1:9000\n", "0.0.0.0:9000", "env HOST"},
		{"both", nil, []string{"PORT=8080", "HOST=::"}, "", "", "[::]:8080", "env PORT and HOST"},
		{"server addr wins", nil, []string{"PORT=8080", "SERVER_ADDR=127.0.0.1:1"}, "", "", "127.0.0.1:1", "env SERVER_ADDR"},
		{"flag wins", []string{"--server-addr", "127.0.0.1:2"}, []string{"PORT=8080"}, "", "", "127.0.0.1:2", "flag --server-addr"},
		{"over tifa.yaml", nil, []string{"PORT=8080"}, "", "server:\n  addr: 127.0.0.1:9000\n", ":8080", "env PORT"},
		{"env port over .env addr", nil, []string{"PORT=8080"}, "SERVER_ADDR=127.0.0.1:3\n", "", ":8080", "env PORT"},
		{".env port", nil, nil, "PORT=3000\n", "", ":3000", ".env PORT"},
		{"empty", nil, []string{"PORT=", "HOST="}, "", "", DefaultAddr, ""},
		{"hostname is not read", nil, []string{"HOSTNAME=box"}, "", "", DefaultAddr, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := ""
			if tc.dotenv != "" {
				dir = dotenvDir(t, tc.dotenv)
			}
			_, settings, err := Project([]byte(tc.project))
			if err != nil {
				t.Fatal(err)
			}
			c, from, err := Load(Read(tc.args, tc.environ, dir), settings)
			if err != nil {
				t.Fatal(err)
			}
			if c.Server.Addr != tc.want || from["server.addr"] != tc.from {
				t.Fatalf("addr %q from %q, want %q from %q", c.Server.Addr, from["server.addr"], tc.want, tc.from)
			}
		})
	}
	for _, bad := range []string{"PORT=http", "PORT=70000", "HOST=a:1"} {
		if _, _, err := Load(Read(nil, []string{bad}, ""), nil); err == nil || !strings.Contains(err.Error(), "server.addr") {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestEveryErrorIsReported(t *testing.T) {
	_, _, err := Load(Read([]string{"--ssr-vms", "x"}, []string{"SSR_TIMEOUT=soon", "CACHE_SIZE=lots"}, ""), nil)
	for _, want := range []string{"flag --ssr-vms", "env SSR_TIMEOUT", "env CACHE_SIZE"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v lacks %q", err, want)
		}
	}
}

func TestProjectGivesTheRuntimeKeysAsText(t *testing.T) {
	c, settings, err := Project([]byte("build:\n  target: es2022\nserver:\n  addr: 0.0.0.0:1\n  compress: false\nshop:\n  open: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Build.Target != "es2022" || c.Server.Addr != "0.0.0.0:1" || c.Server.Compress {
		t.Fatalf("%+v", c)
	}
	if len(settings) != 2 || settings["server.addr"] != "0.0.0.0:1" || settings["server.compress"] != "false" {
		t.Fatalf("settings %q", settings)
	}
}

func TestNamesOfAKey(t *testing.T) {
	if FlagName("server.body_limit") != "server-body-limit" || EnvName("server.body_limit") != "SERVER_BODY_LIMIT" {
		t.Fatal(FlagName("server.body_limit"), EnvName("server.body_limit"))
	}
	for _, good := range []string{"db.url", "jobs.max_workers", "a.b.c", "s3.bucket2"} {
		if err := CheckKey(good); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
	for _, bad := range []string{"db", "DB.url", "db.", ".url", "db..url", "db-url.x", "db.url ", ""} {
		if err := CheckKey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for key, want := range map[string]string{
		"server.addr":   "defined twice",
		"server_addr.x": "",
		"build.target":  "defined twice",
		"ssr.vm.memory": "ssr.vm_memory",
		"lifeline.fd":   "--lifeline-fd",
	} {
		err := Clash(key, []string{"db_url.x"})
		if want == "" {
			if err != nil {
				t.Errorf("%s: %v", key, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) && !strings.Contains(err.Error(), "both have") {
			t.Errorf("%s: %v, want %q", key, err, want)
		}
	}
}

func TestAppSettingsComeFromTheSameSources(t *testing.T) {
	dir := dotenvDir(t, "JOBS_WORKERS=3\n")
	file := writeFile(t, "prod.yaml", "db:\n  url: prod.db\n  pool:\n    idle: 5m\nfeed:\n  hosts: \"a.test, b.test\"\n")
	src := Read([]string{"--shop-open", "serve", "-c", file}, []string{"UPLOAD_LIMIT=2MiB"}, dir)
	r := &registry{}
	var (
		url     = "app.db"
		workers = 1
		open    = false
		idle    = time.Minute
		limit   = int64(1 << 20)
		hosts   []string
		name    = "shop"
	)
	r.define(src, "db.url", "Database file", optionalValue{&url})
	r.define(src, "jobs.workers", "", intValue{p: &workers, min: -1 << 62})
	r.define(src, "shop.open", "", boolValue{&open})
	r.define(src, "db.pool.idle", "", anyDurationValue{&idle})
	r.define(src, "upload.limit", "", sizeValue{&limit, 0})
	r.define(src, "feed.hosts", "", listValue{&hosts})
	r.define(src, "shop.name", "", optionalValue{&name})
	if url != "prod.db" || workers != 3 || !open || idle != 5*time.Minute || limit != 2<<20 || !slices.Equal(hosts, []string{"a.test", "b.test"}) || name != "shop" {
		t.Fatalf("%q %d %v %s %d %q %q", url, workers, open, idle, limit, hosts, name)
	}
	if len(r.errs) != 0 {
		t.Fatal(r.errs)
	}
	from := map[string]string{}
	for _, s := range r.settings {
		from[s.Key] = s.From
	}
	want := map[string]string{"db.url": "file prod.yaml", "jobs.workers": ".env JOBS_WORKERS", "shop.open": "flag --shop-open", "upload.limit": "env UPLOAD_LIMIT", "shop.name": ""}
	for k, v := range want {
		if from[k] != v {
			t.Errorf("%s from %q, want %q", k, from[k], v)
		}
	}
	out := string(Print(Default(), nil, r.settings))
	for _, want := range []string{"db:\n", "  url: \"prod.db\"\n", "  pool:\n", "    idle: 5m0s\n", "Flag --db-pool-idle, env DB_POOL_IDLE. From file prod.yaml.", "From default.", "hosts: \"a.test,b.test\"", "server:\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("printed settings lack %q:\n%s", want, out)
		}
	}
	// What Print writes reads back as a file of -c.
	back := &registry{}
	idle2 := time.Second
	back.define(Read([]string{"-c", writeFile(t, "back.yaml", out)}, nil, ""), "db.pool.idle", "", anyDurationValue{&idle2})
	if idle2 != 5*time.Minute || len(back.errs) != 0 {
		t.Errorf("read back %s, %v", idle2, back.errs)
	}
}

func TestInvalidAppValuesAreRecordedAndGiveTheDefault(t *testing.T) {
	src := Read([]string{"--jobs-workers", "many"}, []string{"SHOP_OPEN=maybe"}, "")
	r := &registry{}
	workers, open := 4, false
	if r.define(src, "jobs.workers", "", intValue{p: &workers, min: -1 << 62}) || workers != 4 {
		t.Fatalf("workers %d", workers)
	}
	if r.define(src, "shop.open", "", boolValue{&open}) || open {
		t.Fatal("open")
	}
	if len(r.errs) != 2 || !strings.Contains(r.errs[0].Error(), "flag --jobs-workers: jobs.workers") || !strings.Contains(r.errs[1].Error(), "env SHOP_OPEN: shop.open") {
		t.Fatal(r.errs)
	}
}

func TestBadAppKeysPanic(t *testing.T) {
	for _, keys := range [][]string{{"db"}, {"db.url", "db.url"}, {"db.url", "db_url.x", "db.url_x"}, {"server.addr"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q did not panic", keys)
				}
			}()
			r := &registry{}
			for _, k := range keys {
				v := ""
				r.define(nil, k, "", optionalValue{&v})
			}
		}()
	}
}

// A test binary keeps the defaults: go test runs it with its own flags and working directory.
func TestTestBinaryKeepsTheDefaults(t *testing.T) {
	if appSources() != nil {
		t.Fatal("a test binary reads the sources")
	}
}

func TestClashOfTwoAppKeys(t *testing.T) {
	if err := Clash("db.url_x", []string{"db_url.x"}); err == nil || !strings.Contains(err.Error(), "both have") {
		t.Fatal(err)
	}
}

// A word that starts with - is a flag and never the value of another one, so a value that
// starts with - is written after =.
func TestAFlagIsNeverTheValueOfAnother(t *testing.T) {
	for _, args := range [][]string{
		{"--server-tls-key", "--server-hsts"},
		{"--server-tls-key", "-cert.pem"},
		{"--server-tls-key", "-"},
		{"--server-tls-key"},
	} {
		_, _, err := Load(Read(args, nil, ""), nil)
		if err == nil || !strings.Contains(err.Error(), "flag --server-tls-key needs a value") {
			t.Errorf("%q: %v", args, err)
		}
	}
	src := Read([]string{"--server-tls-key", "--server-hsts"}, nil, "")
	if err := src.Err(); err != nil {
		t.Errorf("-c read from a flag of another: %v", err)
	}
	c, _, _ := Load(src, nil)
	if !c.Server.HSTS {
		t.Error("--server-hsts did not count")
	}
	c, _, err := Load(Read([]string{"--server-tls-key=-cert.pem", "--server-tls-cert=c.pem", "--server-addr=:443"}, nil, ""), nil)
	if err != nil || c.Server.TLSKey != "-cert.pem" {
		t.Errorf("a value after =: %q, %v", c.Server.TLSKey, err)
	}
	for _, args := range [][]string{{"-c", "--server-hsts"}, {"--config", "-x.yaml"}} {
		if err := Read(args, nil, "").Err(); err == nil || !strings.Contains(err.Error(), "needs a file") {
			t.Errorf("%q: %v", args, err)
		}
	}
	// -cpath is not -c path.
	if err := Read([]string{"-cert.pem"}, nil, "").Err(); err != nil {
		t.Errorf("-cert.pem: %v", err)
	}
}

// A comment after an empty value leaves the value empty.
func TestDotenvCommentAfterAnEmptyValue(t *testing.T) {
	vars, err := parseDotenv([]byte("A= # turn it off\nB=\t# tab\nC=#kept\nD=x\t# tab\n"))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"A": "", "B": "", "C": "#kept", "D": "x"} {
		if got, ok := vars[k]; !ok || got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}
