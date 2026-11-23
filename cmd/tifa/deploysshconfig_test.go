package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testHost = "shop-ssh-k3j6x2m5q7wt.example.com"

// The block is the lines of the design, with Match all closing it, at the top of the file. The
// file the person had follows it byte for byte.
func TestSSHBlockGoesFirstAndKeepsTheRest(t *testing.T) {
	mine := "Include ~/.orbstack/ssh/config\n\nHost *\n  ServerAliveInterval 60\n\n# my box\nHost box\n  HostName 192.0.2.1\n"
	got, err := withSSHBlock(mine, "shop", testHost, "/opt/homebrew/bin/cloudflared", "darwin")
	if err != nil {
		t.Fatal(err)
	}
	want := "# tifa shop " + testHost + ": through the Cloudflare tunnel (written by tifa deploy setup)\n" +
		"Host " + testHost + "\n" +
		"  ProxyCommand /opt/homebrew/bin/cloudflared access ssh --hostname %h\n" +
		"  StrictHostKeyChecking accept-new\n" +
		"Match all\n" +
		"# end tifa shop " + testHost + "\n" + mine
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// A second time changes nothing.
	again, err := withSSHBlock(got, "shop", testHost, "/opt/homebrew/bin/cloudflared", "darwin")
	if err != nil || again != got {
		t.Errorf("a second run changed the file (%v):\n%s", err, again)
	}
	// And the block comes out again with the file as it was.
	rest, hosts, err := cutSSHBlocks(got, exactBlock("shop", testHost))
	if err != nil || len(hosts) != 1 || rest != mine {
		t.Errorf("removed %v %v:\n%q", hosts, err, rest)
	}
}

// A block of tifa that is somewhere else in the file, or has other words, is replaced and moves to
// the top. The blocks of other apps stay.
func TestSSHBlockReplacedAndOthersStay(t *testing.T) {
	other := sshBlock("shop", "shop-ssh-aaaaaaaaaaaa.example.com", "cloudflared", "linux", "\n")
	// A comment of the person that starts like a marker is not a marker.
	own := "# tifa shop: my notes on the shop box\n"
	old := "Host a\n  HostName x\n" + own +
		sshBlock("shop", testHost, "/old/cloudflared", "linux", "\n") +
		other + "Host z\n"
	got, err := withSSHBlock(old, "shop", testHost, "cloudflared", "linux")
	if err != nil {
		t.Fatal(err)
	}
	want := sshBlock("shop", testHost, "cloudflared", "linux", "\n") + "Host a\n  HostName x\n" + own + other + "Host z\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// CRLF files stay CRLF: the block is written with the ending the file uses, and the lines of the
// person are not touched.
func TestSSHBlockCRLF(t *testing.T) {
	mine := "Host box\r\n  HostName 192.0.2.1\r\n"
	got, err := withSSHBlock(mine, "shop", testHost, `C:\Program Files (x86)\cloudflared\cloudflared.exe`, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "\r\n") != 6+2 || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("mixed line endings: %q", got)
	}
	if !strings.Contains(got, `  ProxyCommand "C:/Program Files (x86)/cloudflared/cloudflared.exe" access ssh --hostname %h`+"\r\n") {
		t.Errorf("the path is not quoted:\n%q", got)
	}
	if !strings.HasSuffix(got, mine) {
		t.Errorf("the file of the person changed:\n%q", got)
	}
	rest, hosts, err := cutSSHBlocks(got, exactBlock("shop", testHost))
	if err != nil || len(hosts) != 1 || rest != mine {
		t.Errorf("remove: %v %v %q", hosts, err, rest)
	}
	// A second run changes nothing.
	again, _ := withSSHBlock(got, "shop", testHost, `C:\Program Files (x86)\cloudflared\cloudflared.exe`, "windows")
	if again != got {
		t.Errorf("a second run changed the file:\n%q", again)
	}
}

// A file whose last line has no ending keeps it that way.
func TestSSHBlockLastLineWithoutEnding(t *testing.T) {
	got, err := withSSHBlock("Host a", "shop", testHost, "cloudflared", "linux")
	if err != nil || !strings.HasSuffix(got, "# end tifa shop "+testHost+"\nHost a") {
		t.Errorf("%v %q", err, got)
	}
	rest, hosts, err := cutSSHBlocks(got, exactBlock("shop", testHost))
	if err != nil || len(hosts) != 1 || rest != "Host a" {
		t.Errorf("%v %v %q", hosts, err, rest)
	}
}

// A block that never ends, one that starts inside another, or one whose Host line is not the one
// its first line names is the person's to look at: the error names the line, and nothing is written.
func TestSSHBlockDamaged(t *testing.T) {
	good := sshBlock("shop", testHost, "cloudflared", "linux", "\n")
	begin := sshBlockBegin("shop", testHost)
	for name, c := range map[string]struct{ content, line string }{
		"no end":        {begin + "\nHost " + testHost + "\nHost b\n", "line 1"},
		"inside":        {begin + "\nHost " + testHost + "\n" + good, "line 3"},
		"wrong host":    {"Host a\n" + begin + "\nHost elsewhere\n# end tifa shop " + testHost + "\n", "line 3"},
		"inside, other": {good + begin + "\nHost " + testHost + "\n" + sshBlockBegin("blog", "blog-ssh-aaaaaaaaaaaa.example.com") + "\n", "line 9"},
	} {
		_, err := withSSHBlock(c.content, "shop", testHost, "cloudflared", "linux")
		if err == nil || !strings.Contains(err.Error(), c.line) {
			t.Errorf("%s: got %v, want an error with %s", name, err, c.line)
		}
	}
}

// Blocks are named by the host: another host of the same app keeps its own, and removal by domain
// takes only the hosts of that domain.
func TestSSHBlocksByHost(t *testing.T) {
	a := sshBlock("shop", testHost, "cloudflared", "linux", "\n")
	b := sshBlock("shop", "shop-ssh-aaaaaaaaaaaa.example.org", "cloudflared", "linux", "\n")
	rest, hosts, err := cutSSHBlocks(a+b+"Host z\n", domainBlocks("shop", "shop.example.com"))
	if err != nil || len(hosts) != 1 || hosts[0] != testHost || rest != b+"Host z\n" {
		t.Errorf("%v %v %q", hosts, err, rest)
	}
	// Writing one host leaves the other where it is.
	got, err := withSSHBlock(b, "shop", testHost, "cloudflared", "linux")
	if err != nil || got != a+b {
		t.Errorf("%v\n%s", err, got)
	}
}

func TestProxyProgram(t *testing.T) {
	for _, c := range []struct{ path, goos, want string }{
		{"/opt/homebrew/bin/cloudflared", "darwin", "/opt/homebrew/bin/cloudflared"},
		{"/Users/a b/bin/cloudflared", "linux", `"/Users/a b/bin/cloudflared"`},
		{`/x/it"s $HOME/cf`, "linux", `"/x/it\"s \$HOME/cf"`},
		{`C:\Program Files (x86)\cloudflared\cloudflared.exe`, "windows", `"C:/Program Files (x86)/cloudflared/cloudflared.exe"`},
		{`C:\Tools\cloudflared.exe`, "windows", `C:/Tools/cloudflared.exe`},
		{"/100%/cf", "linux", "/100%%/cf"},
	} {
		if got := proxyProgram(c.path, c.goos); got != c.want {
			t.Errorf("%s on %s: got %s, want %s", c.path, c.goos, got, c.want)
		}
	}
}

// The file is made when it is missing, with mode 0600 in a folder with mode 0700, rewritten in
// place on a later run, and the block leaves with remove.
func TestSSHBlockFile(t *testing.T) {
	home := t.TempDir()
	old := sshHome
	sshHome = func() (string, error) { return home, nil }
	t.Cleanup(func() { sshHome = old })
	path, err := sshConfigPath()
	if err != nil || path != filepath.Join(home, ".ssh", "config") {
		t.Fatalf("%q %v", path, err)
	}
	same, present, err := sshBlockState(path, "shop", testHost, "cloudflared")
	if err != nil || same || present {
		t.Fatalf("a missing file: %v %v %v", same, present, err)
	}
	if err := writeSSHBlock(path, "shop", testHost, "cloudflared"); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
			if info, err := os.Stat(p); err != nil || info.Mode().Perm() != want {
				t.Errorf("%s: %v %v", p, info, err)
			}
		}
	}
	same, present, err = sshBlockState(path, "shop", testHost, "cloudflared")
	if err != nil || !same || !present {
		t.Errorf("after the write: %v %v %v", same, present, err)
	}
	// Another path to the program is a change.
	if same, _, _ := sshBlockState(path, "shop", testHost, "/usr/bin/cloudflared"); same {
		t.Error("a new path to cloudflared is not a change")
	}
	// The person adds a line, the block is written again, and their line stays.
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(data, "Host mine\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSSHBlock(path, "shop", testHost, "/usr/bin/cloudflared"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "ProxyCommand /usr/bin/cloudflared") || !strings.HasSuffix(string(data), "Host mine\n") {
		t.Errorf("file:\n%s", data)
	}
	hosts, err := removeSSHBlocks(path, "shop", "shop.example.com")
	if err != nil || len(hosts) != 1 {
		t.Fatalf("%v %v", hosts, err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "Host mine\n" {
		t.Errorf("after remove:\n%q", data)
	}
	if hosts, err := removeSSHBlocks(path, "shop", "shop.example.com"); err != nil || len(hosts) != 0 {
		t.Errorf("a second remove: %v %v", hosts, err)
	}
}
