package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func load(t *testing.T, args, environ []string, embeddedName, embedded string) (Config, error) {
	t.Helper()
	fs := pflag.NewFlagSet("serve", pflag.ContinueOnError)
	AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return Load(fs, environ, embeddedName, []byte(embedded))
}

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
	environ := []string{"PATH=/bin", "TIFA_SSR_VMS=7", "TIFA_SSR_TIMEOUT=6s"}
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

func TestEmbeddedJSONIsAccepted(t *testing.T) {
	c, err := load(t, nil, nil, "tifa.json", `{"server":{"addr":":9528","shutdown_timeout":"2s"},"ssr":{"vms":1}}`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Addr != ":9528" || c.Server.ShutdownTimeout != 2*time.Second || c.SSR.VMs != 1 {
		t.Fatalf("%+v", c)
	}
}

// build.skip_tanstack_check is a true or false of the project file, off by default.
func TestSkipTanStackCheckIsABoolOfTheProjectFile(t *testing.T) {
	if Default().Build.SkipTanStackCheck {
		t.Fatal("the check is skipped by default")
	}
	for name, content := range map[string]string{
		"tifa.yaml": "build:\n  skip_tanstack_check: true\n",
		"tifa.json": `{"build":{"skip_tanstack_check":true}}`,
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
		"tifa.json": `{"build":{"skip_tanstack_check":1}}`,
	} {
		if _, err := load(t, nil, nil, name, content); err == nil || !strings.Contains(err.Error(), "build.skip_tanstack_check") || !strings.Contains(err.Error(), "expected a bool value") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	_, err := load(t, []string{"--config", writeFile(t, "prod.yaml", "build:\n  skip_tanstack_check: true\n")}, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "build settings belong in the project tifa.yaml") {
		t.Errorf("in a deployment file: %v", err)
	}
}

// build.pgo is a string of the project file, auto by default. YAML reads a plain off as text.
// A value that looks like a switch but is not auto or off says what to write.
func TestPGOIsAStringOfTheProjectFile(t *testing.T) {
	if got := Default().Build.PGO; got != "auto" {
		t.Fatalf("default build.pgo = %q", got)
	}
	for name, content := range map[string]string{
		"tifa.yaml": "build:\n  pgo: off\n",
		"tifa.json": `{"build":{"pgo":"off"}}`,
	} {
		c, err := load(t, nil, nil, name, content)
		if err != nil || c.Build.PGO != "off" {
			t.Errorf("%s: %+v, %v", name, c.Build, err)
		}
	}
	if c, err := load(t, nil, nil, "tifa.yaml", "build:\n  pgo: profiles/prod.pgo\n"); err != nil || c.Build.PGO != "profiles/prod.pgo" {
		t.Errorf("a file: %+v, %v", c.Build, err)
	}
	// Values that look like a switch, which would otherwise be read as a file name.
	for _, value := range []string{"false", "'false'", "OFF", "no", "none", "disabled", "0"} {
		if _, err := load(t, nil, nil, "tifa.yaml", "build:\n  pgo: "+value+"\n"); err == nil || !strings.Contains(err.Error(), "build.pgo") || !strings.Contains(err.Error(), "to turn it off, write off") {
			t.Errorf("%s: %v", value, err)
		}
	}
	for _, value := range []string{"true", "yes", "on", "Auto"} {
		if _, err := load(t, nil, nil, "tifa.yaml", "build:\n  pgo: "+value+"\n"); err == nil || !strings.Contains(err.Error(), "build.pgo") || !strings.Contains(err.Error(), "write auto") {
			t.Errorf("%s: %v", value, err)
		}
	}
	if _, err := load(t, nil, nil, "tifa.json", `{"build":{"pgo":false}}`); err == nil || !strings.Contains(err.Error(), "to turn it off, write off") {
		t.Errorf("JSON false: %v", err)
	}
	_, err := load(t, []string{"--config", writeFile(t, "prod.yaml", "build:\n  pgo: off\n")}, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "build settings belong in the project tifa.yaml") {
		t.Errorf("in a deployment file: %v", err)
	}
}

func TestNoFilesGivesDefaults(t *testing.T) {
	c, err := load(t, nil, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c != Default() {
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
		{name: "env type", environ: []string{"TIFA_SERVER_SHUTDOWN_TIMEOUT=soon"}, want: []string{"env TIFA_SERVER_SHUTDOWN_TIMEOUT", "server.shutdown_timeout"}},
		{name: "env unknown", environ: []string{"TIFA_SERVER_ADR=:1"}, want: []string{"env TIFA_SERVER_ADR", "unknown key"}},
		{name: "env build", environ: []string{"TIFA_BUILD_TARGET=es2022"}, want: []string{"env TIFA_BUILD_TARGET", "unknown key"}},
		{name: "file unknown", file: "server:\n  adr: :1\n", want: []string{"file prod.yaml line 2", `unknown key "server.adr"`}},
		{name: "file section", file: "sever:\n  addr: :1\n", want: []string{"file prod.yaml line 1", `unknown key "sever"`}},
		{name: "file unit", file: "cache:\n  size: 64MB\n", want: []string{"file prod.yaml line 2", "cache.size", `"64MB"`}},
		{name: "file range", file: "ssr:\n  vms: 0\n", want: []string{"file prod.yaml line 2", "ssr.vms"}},
		{name: "file list", file: "server:\n  addr: [a, b]\n", want: []string{"file prod.yaml line 2", "server.addr"}},
		{name: "file build", file: "build:\n  target: es2022\n", want: []string{"file prod.yaml line 2", "build settings"}},
		{name: "embedded type", embedded: "ssr:\n  timeout: 3\n", want: []string{"embedded tifa.yaml line 2", "ssr.timeout"}},
		{name: "embedded syntax", embedded: "ssr: [\n", want: []string{"embedded tifa.yaml"}},
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

func TestJSONFileErrorsNameKey(t *testing.T) {
	file := writeFile(t, "prod.json", `{"ssr":{"vm_memory":true}}`)
	_, err := load(t, []string{"--config", file}, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "file prod.json") || !strings.Contains(err.Error(), "ssr.vm_memory") {
		t.Fatalf("got %v", err)
	}
	file = writeFile(t, "prod.toml", "")
	if _, err := load(t, []string{"--config", file}, nil, "", ""); err == nil {
		t.Fatal("unsupported format must fail")
	}
}

func TestExampleLoadsBackToSameValues(t *testing.T) {
	c := Default()
	c.Server.Addr = "0.0.0.0:80"
	c.Cache.Size = 3 << 30
	c.SSR.VMMemory = 1536 << 10
	example := Example(c)
	for _, want := range []string{"--server-addr", "TIFA_SERVER_ADDR", "server:", "body_limit: 1MiB"} {
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
	if got != c {
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
		{"json number addr", "prod.json", `{"server":{"addr":9527}}`, "server.addr"},
		{"json string vms", "prod.json", `{"ssr":{"vms":"2"}}`, "ssr.vms"},
		{"json null addr", "prod.json", `{"server":{"addr":null}}`, "server.addr"},
		{"json fraction vms", "prod.json", `{"ssr":{"vms":1.5}}`, "ssr.vms"},
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
	if _, err := load(t, []string{"--config", writeFile(t, "ok.json", `{"server":{"body_limit":1048576}}`)}, nil, "", ""); err != nil {
		t.Errorf("json byte count: %v", err)
	}
}

func TestFileMustHoldOneDocument(t *testing.T) {
	cases := map[string]string{
		"prod.json": `{} {"ssr":{"vms":0}}`,
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
	want := "build.target build.public build.skip_tanstack_check build.pgo server.addr server.body_limit server.shutdown_timeout ssr.vms ssr.vm_memory ssr.vm_stack_depth ssr.timeout cache.size"
	if got := strings.Join(Keys(), " "); got != want {
		t.Fatalf("Keys: %s, want %s", got, want)
	}
}

// tifa dev and a built app both listen on 9527 unless told otherwise.
func TestDefaultAddr(t *testing.T) {
	if DefaultAddr != "127.0.0.1:9527" || Default().Server.Addr != DefaultAddr {
		t.Fatalf("DefaultAddr = %q, Default().Server.Addr = %q", DefaultAddr, Default().Server.Addr)
	}
}

// ssr.vm_stack_depth is a whole number from 1 to 10000, 10000 by default: the default of the
// QuickJS fork, which internal/ssr checks. A deeper stack would let code that recurses without
// end grow the goroutine stack past Go's limit, which ends the process.
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
		{"env", "env TIFA_SSR_VM_STACK_DEPTH", nil, []string{"TIFA_SSR_VM_STACK_DEPTH=10001"}, ""},
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
		for _, want := range []string{tc.layer, "ssr.vm_stack_depth", "above the maximum 10000", "Go's limit", "ends the process"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %v lacks %q", tc.name, err, want)
			}
		}
	}
	_, err = load(t, nil, nil, "tifa.yaml", "ssr:\n  vm_stack_depth: 20000\n")
	if err == nil || !strings.Contains(err.Error(), "embedded tifa.yaml") || !strings.Contains(err.Error(), "above the maximum 10000") {
		t.Errorf("project file: %v", err)
	}
	c, err = load(t, []string{"--ssr-vm-stack-depth", "3000"}, []string{"TIFA_SSR_VM_STACK_DEPTH=2000"}, "tifa.yaml", "ssr:\n  vm_stack_depth: 1000\n")
	if err != nil || c.SSR.VMStackDepth != 3000 {
		t.Fatalf("flag: %d, %v", c.SSR.VMStackDepth, err)
	}
	c, err = load(t, nil, []string{"TIFA_SSR_VM_STACK_DEPTH=2000"}, "tifa.yaml", "ssr:\n  vm_stack_depth: 1000\n")
	if err != nil || c.SSR.VMStackDepth != 2000 {
		t.Fatalf("env: %d, %v", c.SSR.VMStackDepth, err)
	}
	_, err = load(t, nil, []string{"TIFA_SSR_VM_STACK_DEPTH=0"}, "", "")
	if err == nil || !strings.Contains(err.Error(), "ssr.vm_stack_depth") || !strings.Contains(err.Error(), "below the minimum 1") {
		t.Fatalf("0: %v", err)
	}
}
