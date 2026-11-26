package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// recordingSSHEnv makes the test binary an ssh that writes its arguments to args.json and its input
// to stdin in the folder the variable names, see recordSSH.
const recordingSSHEnv = "TIFA_TEST_RECORDING_SSH"

// recordSSH is the ssh of recordingSSHEnv. It exits.
func recordSSH(dir string) {
	args, _ := json.Marshal(os.Args[1:])
	in, err := io.ReadAll(os.Stdin)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "args.json"), args, 0o644)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "stdin"), in, 0o644)
	}
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

// useRecordingSSH makes the deploy commands run the recording ssh, and returns the folder it
// writes to.
func useRecordingSSH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(recordingSSHEnv, dir)
	old := sshProgram
	sshProgram = os.Args[0]
	t.Cleanup(func() { sshProgram = old })
	return dir
}

// The server gets the script exactly as written, whatever it holds, on every system: on Windows
// the command line of ssh is one string that Go quotes and ssh splits again.
func TestDeploySSHPassesTheScriptUnchanged(t *testing.T) {
	dir := useRecordingSSH(t)
	script := "set -eu\nprintf '%s\\n' \"$HOME\" 'it''s' \"a \\\"quote\\\"\"\n\techo %PATH% ^& | > < `id` C:\\dir\\ \\\\\\\n" +
		"echo \"ends with a backslash\\\\\"\nécho ünïcode\r\nlast line\\"
	old := stdinIsTerminal
	stdinIsTerminal = func(io.Reader) bool { return false }
	t.Cleanup(func() { stdinIsTerminal = old })
	tgt := deployTarget{host: "vps.example.com"}
	if err := tgt.ssh(context.Background(), deployUser, script, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "args.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"-l", deployUser, "-o", "BatchMode=yes", "--", "vps.example.com", "sh -c " + shellQuote(script)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ssh got the arguments\n%q\nwant\n%q", got, want)
	}
}

// The upload sends the program through the input of ssh byte for byte, gzipped, and the script
// carries its SHA-256 sum, which the server checks. Bytes that a text mode would change, such as
// CRLF and Ctrl-Z, arrive as they are.
func TestDeploySSHUploadsTheProgramUnchanged(t *testing.T) {
	dir := useRecordingSSH(t)
	program := filepath.Join(t.TempDir(), "shop")
	content := bytes.Repeat([]byte("\x7fELF\r\n\n\r\x1a\x00\xff text\r\n"), 50000)
	if err := os.WriteFile(program, content, 0o755); err != nil {
		t.Fatal(err)
	}
	tgt := deployTarget{host: "vps", app: "shop"}
	if err := tgt.upload(context.Background(), program, "20261122T000000Z", io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	gz, err := os.Open(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	r, err := gzip.NewReader(gz)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("the server got %d bytes (%v), want the %d of the program", len(got), err, len(content))
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.json"))
	sum := sha256.Sum256(content)
	if !strings.Contains(string(args), hex.EncodeToString(sum[:])) {
		t.Errorf("the script lacks the sum of the program:\n%s", args)
	}
}

// With an admin user in the host, the steps of root log in as that user and run through sudo -n,
// and their input still reaches the script. The steps of tifa log in as tifa. ssh gets the host
// without the user either way.
func TestDeploySSHAdminUser(t *testing.T) {
	home := t.TempDir()
	calls := fakeServer(t, home, nil)
	ctx := context.Background()
	target := func(host string) deployTarget {
		h, err := parseDeployHost(host)
		if err != nil {
			t.Fatal(err)
		}
		return deployTarget{host: h.addr(), dest: h}
	}
	tgt := target("ubuntu@vps")
	var out bytes.Buffer
	if err := tgt.ssh(ctx, "root", "echo ran; cat", strings.NewReader("token"), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ran\ntoken" {
		t.Errorf("the script printed %q", out.String())
	}
	if c := calls(); !strings.Contains(c, "ssh ubuntu vps\n") || !strings.Contains(c, "sudo -n true\n") || !strings.Contains(c, "sudo -n -H sh -c ") {
		t.Errorf("root step:\n%s", c)
	}
	before := strings.Count(calls(), "sudo ")
	if err := tgt.ssh(ctx, deployUser, "echo hi", nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if c := calls(); !strings.Contains(c, "ssh tifa vps\n") || strings.Count(c, "sudo ") != before {
		t.Errorf("tifa step:\n%s", c)
	}

	// Without passwordless sudo the script never runs, and the error says what is needed.
	writeFile(t, filepath.Join(home, ".run", "nosudo"), "")
	out.Reset()
	err := tgt.ssh(ctx, "root", "echo ran", nil, &out, io.Discard)
	var noSudo *noSudoError
	if !errors.As(err, &noSudo) || out.Len() > 0 {
		t.Fatalf("got %v, printed %q", err, out.String())
	}
	for _, want := range []string{"ubuntu", "passwordless sudo", "NOPASSWD", "without a user", "requiretty"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q lacks %q", err, want)
		}
	}

	// root@ is root as before: no sudo.
	before = strings.Count(calls(), "sudo ")
	if err := target("root@vps").ssh(ctx, "root", "echo ran", nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if c := calls(); !strings.Contains(c, "ssh root vps\n") || strings.Count(c, "sudo ") != before {
		t.Errorf("root@:\n%s", c)
	}
}

// Setup gives the deploy user the keys of the admin user, whose home getent knows, and adds each
// key once.
func TestDeployCopiesTheKeysOfTheAdmin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the server scripts run under sh")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	tool := func(name, body string) {
		writeFile(t, filepath.Join(bin, name), "#!/bin/sh\n"+body+"\n")
		if err := os.Chmod(filepath.Join(bin, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	admin, tifa := filepath.Join(dir, "ubuntu"), filepath.Join(dir, "tifa")
	tool("getent", `[ "$1 $2" = "passwd ubuntu" ] && echo "ubuntu:x:1000:1000::`+admin+`:/bin/bash"`)
	tool("install", `for a; do last=$a; done; mkdir -p "$last"`)
	tool("chown", "true")
	writeFile(t, filepath.Join(admin, ".ssh", "authorized_keys"), "# keys\nssh-ed25519 AAAAone me@laptop\n\nssh-ed25519 AAAAtwo other@desk")
	writeFile(t, filepath.Join(tifa, ".ssh", "authorized_keys"), "ssh-ed25519 AAAAone me@laptop\n")
	c := exec.Command("sh", "-c", script(copyKeysBody, "user", deployUser, "home", tifa, "admin", "ubuntu"))
	c.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, _ := os.ReadFile(filepath.Join(tifa, ".ssh", "authorized_keys"))
	if want := "ssh-ed25519 AAAAone me@laptop\nssh-ed25519 AAAAtwo other@desk\n"; string(data) != want {
		t.Errorf("authorized_keys of tifa:\n%s\nwant\n%s", data, want)
	}
	// An admin user the server does not know stops the script.
	c = exec.Command("sh", "-c", script(copyKeysBody, "user", deployUser, "home", tifa, "admin", "nobody-here"))
	c.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	if out, err := c.CombinedOutput(); err == nil || !strings.Contains(string(out), "nobody-here") {
		t.Errorf("unknown admin: %v: %s", err, out)
	}
	// Without an admin user the keys are those of root, as before.
	if !strings.Contains(copyKeysBody, "/root/.ssh/authorized_keys") {
		t.Error("root's keys are not read")
	}
}

// fakeSSHConfig makes ssh a script that answers ssh -G with config, and for a login logs its
// arguments to the file it returns, prints said to stderr and exits with code.
func fakeSSHConfig(t *testing.T, config, said string, code int) (argv string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh is a shell script")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config"), config)
	writeFile(t, filepath.Join(dir, "said"), said)
	argv = filepath.Join(dir, "argv")
	prog := filepath.Join(dir, "ssh")
	writeFile(t, prog, "#!/bin/sh\nif [ \"$1\" = -G ]; then cat "+shellQuote(filepath.Join(dir, "config"))+"; exit 0; fi\n"+
		"printf '%s\\n' \"$@\" > "+shellQuote(argv)+"\ncat "+shellQuote(filepath.Join(dir, "said"))+" >&2\nexit "+strconv.Itoa(code)+"\n")
	if err := os.Chmod(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	old := sshProgram
	sshProgram = prog
	t.Cleanup(func() { sshProgram = old })
	return argv
}

func sshTarget(t *testing.T, host string) deployTarget {
	t.Helper()
	h, err := parseDeployHost(host)
	if err != nil {
		t.Fatal(err)
	}
	return deployTarget{host: h.addr(), dest: h}
}

// Without a terminal ssh cannot ask anything, so it runs with BatchMode and fails at once instead
// of waiting for an answer. A server ssh reaches directly, with no ConnectTimeout of your own,
// gets one, so a server that is off fails in seconds. A ProxyCommand or ProxyJump, such as the
// login of Cloudflare Access, may wait for you in a browser, and gets none.
func TestDeploySSHOptions(t *testing.T) {
	direct := "hostname 203.0.113.7\nport 22\nconnecttimeout none\nproxycommand none\n"
	for _, tc := range []struct {
		name, config string
		terminal     bool
		want, not    []string
	}{
		{"no terminal, direct", direct, false, []string{"BatchMode=yes", "ConnectTimeout=15"}, nil},
		{"terminal, direct", direct, true, []string{"ConnectTimeout=15"}, []string{"BatchMode=yes"}},
		{"own timeout", "hostname 203.0.113.7\nconnecttimeout 40\n", false, []string{"BatchMode=yes"}, []string{"ConnectTimeout=15"}},
		{"proxy command", "hostname vps.example.com\nconnecttimeout none\nproxycommand cloudflared access ssh --hostname %h\n", false, nil, []string{"ConnectTimeout=15"}},
		{"proxy jump", "hostname 10.0.0.7\nconnecttimeout none\nproxyjump bastion\n", false, nil, []string{"ConnectTimeout=15"}},
	} {
		argv := fakeSSHConfig(t, tc.config, "", 0)
		old := stdinIsTerminal
		stdinIsTerminal = func(io.Reader) bool { return tc.terminal }
		err := sshTarget(t, "vps").ssh(context.Background(), deployUser, "true", nil, io.Discard, io.Discard)
		stdinIsTerminal = old
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		data, _ := os.ReadFile(argv)
		args := strings.Split(strings.TrimSpace(string(data)), "\n")
		if args[0] != "-l" || args[1] != deployUser || !slices.Contains(args, "--") {
			t.Errorf("%s: ssh got %q", tc.name, args)
		}
		for _, w := range tc.want {
			if !slices.Contains(args, w) {
				t.Errorf("%s: ssh got %q, want %s", tc.name, args, w)
			}
		}
		for _, n := range tc.not {
			if slices.Contains(args, n) {
				t.Errorf("%s: ssh got %q, want no %s", tc.name, args, n)
			}
		}
	}
}

// A host key ssh does not know yet, or one that changed, is a login that fails with exit 255. The
// error says what to do, with the host as known_hosts names it.
func TestDeploySSHHostKeyErrors(t *testing.T) {
	unknown := "No ED25519 host key is known for 203.0.113.7 and you have requested strict checking.\r\nHost key verification failed.\r\n"
	changed := "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n" +
		"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\nHost key verification failed.\n"
	for _, tc := range []struct {
		name, host, said string
		want             []string
	}{
		{"unknown", "vps", unknown, []string{"not in your known_hosts", "ssh -o StrictHostKeyChecking=accept-new -l tifa vps true"}},
		{"unknown with port", "ssh://203.0.113.7:2222", unknown, []string{"ssh -o StrictHostKeyChecking=accept-new -l tifa ssh://203.0.113.7:2222 true"}},
		{"changed", "vps", changed, []string{"changed", "ssh-keygen -R vps", "do not go on"}},
		{"changed with port", "ssh://ubuntu@203.0.113.7:2222", changed, []string{"ssh-keygen -R '[203.0.113.7]:2222'"}},
		{"timed out", "vps", "ssh: connect to host 203.0.113.7 port 22: Operation timed out\r\n", []string{"ssh could not reach vps", "the server is on"}},
		{"refused", "vps", "ssh: connect to host 203.0.113.7 port 22: Connection refused\r\n", []string{"ssh could not reach vps"}},
		{"no name", "vps", "ssh: Could not resolve hostname vps: nodename nor servname provided, or not known\r\n", []string{"ssh could not reach vps"}},
	} {
		fakeSSHConfig(t, "hostname 203.0.113.7\n", tc.said, 255)
		var said bytes.Buffer
		err := sshTarget(t, tc.host).ssh(context.Background(), deployUser, "true", nil, io.Discard, &said)
		var login *sshLoginError
		if !errors.As(err, &login) {
			t.Fatalf("%s: got %v", tc.name, err)
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: %q lacks %q", tc.name, err, w)
			}
		}
		if strings.Contains(err.Error(), "run go tool tifa deploy setup first") {
			t.Errorf("%s: %q sends you to setup", tc.name, err)
		}
		if said.String() != tc.said {
			t.Errorf("%s: the caller got %q of what ssh printed", tc.name, said.String())
		}
	}
}
