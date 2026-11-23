package main

// The block that tifa deploy setup keeps in the ~/.ssh/config of the developer, so ssh reaches the
// server through the Cloudflare Tunnel (deploy.connect cloudflare):
//
//	# tifa shop <ssh host>: through the Cloudflare tunnel (written by tifa deploy setup)
//	Host <ssh host>
//	  ProxyCommand <cloudflared> access ssh --hostname %h
//	  StrictHostKeyChecking accept-new
//	Match all
//	# end tifa shop <ssh host>
//
// Tifa owns the lines from the first marker to the last and nothing else in the file. The markers
// name the app and the ssh host, and are matched whole: another comment that starts with "# tifa"
// is the person's. Every other
// byte stays as it was, the line endings of the file included. The block goes to the top of the
// file, because ssh takes the first value it sees for an option: a Host * further down cannot
// replace the ProxyCommand. Match all closes the block, so the lines of the person that were
// outside any Host before (an Include, a global option) are outside again.
//
// StrictHostKeyChecking accept-new is there because the first connection through the tunnel
// has no known host key yet, and ssh cannot ask the question without a terminal. A key that
// changes later is still refused.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sshHome finds the folder that holds .ssh: the home of the person. Tests replace it.
var sshHome = os.UserHomeDir

// sshConfigPath is the ssh config file of the person.
func sshConfigPath() (string, error) {
	home, err := sshHome()
	if err != nil {
		return "", fmt.Errorf("cannot find your home folder, which holds .ssh/config: %w", err)
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// sshBeginWords is what follows the app and the ssh host in the first line of the block.
const sshBeginWords = ": through the Cloudflare tunnel (written by tifa deploy setup)"

// The block is named by the app and the ssh host name, so two apps with one name, or one app with
// two hosts, keep their own blocks.
func sshBlockBegin(app, host string) string { return "# tifa " + app + " " + host + sshBeginWords }

func sshBlockEnd(app, host string) string { return "# end tifa " + app + " " + host }

// parseSSHBegin reads the first line of a block, exactly as tifa writes it. Any other comment
// that begins with "# tifa" is the person's own.
func parseSSHBegin(line string) (app, host string, ok bool) {
	rest, ok := strings.CutPrefix(line, "# tifa ")
	if !ok {
		return "", "", false
	}
	rest, ok = strings.CutSuffix(rest, sshBeginWords)
	if !ok {
		return "", "", false
	}
	app, host, ok = strings.Cut(rest, " ")
	if !ok || app == "" || host == "" || strings.ContainsAny(host, " \t") {
		return "", "", false
	}
	return app, host, true
}

// proxyProgram writes the program of a ProxyCommand: bare when its path needs no quoting, else
// in the quotes the shell that runs it reads. ssh runs the command with sh on Unix and with
// cmd.exe on Windows. A % is doubled, since ssh reads %h and the like in the line.
func proxyProgram(path, goos string) string {
	if goos == "windows" {
		// Windows OpenSSH runs the command with cmd.exe, and Git for Windows ssh with sh. Both read
		// C:/Tools/cloudflared.exe, and neither reads a backslash the same way.
		path = strings.ReplaceAll(path, `\`, "/")
	}
	path = strings.ReplaceAll(path, "%", "%%")
	if !strings.ContainsAny(path, " \t\"'\\$`&|;<>()*?[]{}!#~^") {
		return path
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(path) + `"`
}

// sshBlock is the text of the block, with eol after each line.
func sshBlock(app, host, cloudflared, goos, eol string) string {
	lines := []string{
		sshBlockBegin(app, host),
		"Host " + host,
		"  ProxyCommand " + proxyProgram(cloudflared, goos) + " access ssh --hostname %h",
		"  StrictHostKeyChecking accept-new",
		"Match all",
		sshBlockEnd(app, host),
	}
	return strings.Join(lines, eol) + eol
}

// fileEOL is the line ending the file uses: CRLF when its first line ends so, else LF. An empty
// file gets the ending of the system.
func fileEOL(content, goos string) string {
	if i := strings.IndexByte(content, '\n'); i >= 0 {
		if i > 0 && content[i-1] == '\r' {
			return "\r\n"
		}
		return "\n"
	}
	if content == "" && goos == "windows" {
		return "\r\n"
	}
	return "\n"
}

// cutSSHBlocks takes the blocks of tifa that match out of content, and returns the rest and the
// hosts of the blocks it cut. Every block of tifa is checked, also the ones that stay: a block
// whose first line is not followed by its Host line, that starts inside another block or that
// never ends is an error that names the line, and nothing is cut. The file is the person's and
// tifa does not guess what to delete.
func cutSSHBlocks(content string, match func(app, host string) bool) (rest string, hosts []string, err error) {
	var out strings.Builder
	var cur struct {
		app, host string
		start     int
		cut       bool
		hostLine  bool
	}
	inBlock := false
	for n := 1; len(content) > 0; n++ {
		line, next, _ := strings.Cut(content, "\n")
		whole := content[:len(content)-len(next)]
		if next == "" && !strings.HasSuffix(whole, "\n") {
			whole = line // the last line has no ending
		}
		trimmed := strings.TrimRight(line, "\r")
		app, host, begins := parseSSHBegin(trimmed)
		switch {
		case begins && inBlock:
			return "", nil, fmt.Errorf("ssh config line %d starts a block of tifa inside the block that starts at line %d: delete one of them, and run setup again", n, cur.start)
		case begins:
			inBlock = true
			cur.app, cur.host, cur.start, cur.hostLine = app, host, n, false
			cur.cut = match(app, host)
			if cur.cut {
				hosts = append(hosts, host)
			}
		case inBlock && !cur.hostLine:
			if trimmed != "Host "+cur.host {
				return "", nil, fmt.Errorf("ssh config line %d: the block of tifa %s that starts at line %d must go on with %q, and has %q: delete the block, and run setup again", n, cur.app, cur.start, "Host "+cur.host, trimmed)
			}
			cur.hostLine = true
		case inBlock && trimmed == sshBlockEnd(cur.app, cur.host):
			inBlock = false
			if !cur.cut {
				out.WriteString(whole)
			}
			content = next
			continue
		}
		if !(inBlock && cur.cut) {
			out.WriteString(whole)
		}
		content = next
	}
	if inBlock {
		return "", nil, fmt.Errorf("ssh config line %d starts a block of tifa that never ends with %q: delete it, and run setup again", cur.start, sshBlockEnd(cur.app, cur.host))
	}
	return out.String(), hosts, nil
}

// exactBlock matches the block of one app and one ssh host name.
func exactBlock(app, host string) func(string, string) bool {
	return func(a, h string) bool { return a == app && h == host }
}

// domainBlocks matches the blocks of an app for the ssh host names of a domain.
func domainBlocks(app, domain string) func(string, string) bool {
	return func(a, h string) bool { return a == app && looksLikeSSHHost(h, domain) }
}

// withSSHBlock returns content with the block of app and host first and nothing else changed.
func withSSHBlock(content, app, host, cloudflared, goos string) (string, error) {
	rest, _, err := cutSSHBlocks(content, exactBlock(app, host))
	if err != nil {
		return "", err
	}
	return sshBlock(app, host, cloudflared, goos, fileEOL(content, goos)) + rest, nil
}

// readSSHConfig reads the file, or "" when there is none.
func readSSHConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// sshBlockState says what the file has of the block of app and host: whether the file holds
// exactly the text withSSHBlock would give, and whether it holds the block at all.
func sshBlockState(path, app, host, cloudflared string) (same, present bool, err error) {
	content, err := readSSHConfig(path)
	if err != nil {
		return false, false, err
	}
	_, hosts, err := cutSSHBlocks(content, exactBlock(app, host))
	if err != nil {
		return false, false, err
	}
	want, err := withSSHBlock(content, app, host, cloudflared, toolOS)
	return want == content, len(hosts) > 0, err
}

// writeSSHBlock puts the block of app and host at the top of the file at path. A file that is
// not there is made with mode 0600, and the folder with 0700. A file that is there keeps its
// mode, and is written in place, so a link to a file of a dotfiles folder stays a link.
func writeSSHBlock(path, app, host, cloudflared string) error {
	content, err := readSSHConfig(path)
	if err != nil {
		return err
	}
	text, err := withSSHBlock(content, app, host, cloudflared, toolOS)
	if err != nil {
		return err
	}
	if text == content {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o600)
}

// sshBlocksFor lists the ssh host names of the blocks of app for the domain.
func sshBlocksFor(path, app, domain string) ([]string, error) {
	content, err := readSSHConfig(path)
	if err != nil {
		return nil, err
	}
	_, hosts, err := cutSSHBlocks(content, domainBlocks(app, domain))
	return hosts, err
}

// removeSSHBlocks deletes the blocks of app for the domain from the file, and lists their hosts.
func removeSSHBlocks(path, app, domain string) ([]string, error) {
	content, err := readSSHConfig(path)
	if err != nil {
		return nil, err
	}
	rest, hosts, err := cutSSHBlocks(content, domainBlocks(app, domain))
	if err != nil || len(hosts) == 0 {
		return nil, err
	}
	return hosts, os.WriteFile(path, []byte(rest), 0o600)
}
