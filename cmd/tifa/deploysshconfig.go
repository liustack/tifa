package main

// The block that tifa deploy setup keeps in the ~/.ssh/config of the developer, so ssh reaches the
// server through the Cloudflare Tunnel (deploy.ssh cloudflare):
//
//	# tifa shop: through the Cloudflare tunnel (written by tifa deploy setup)
//	Host <ssh host>
//	  ProxyCommand <cloudflared> access ssh --hostname %h
//	  StrictHostKeyChecking accept-new
//	Match all
//	# end tifa shop
//
// Tifa owns the lines from the first marker to the last and nothing else in the file. Every other
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

func sshBlockBegin(app string) string {
	return "# tifa " + app + ": through the Cloudflare tunnel (written by tifa deploy setup)"
}

func sshBlockEnd(app string) string { return "# end tifa " + app }

// sshBlockStart is how the first line of the block of app starts, whatever words follow.
func sshBlockStart(app string) string { return "# tifa " + app + ":" }

// proxyProgram writes the program of a ProxyCommand: bare when its path needs no quoting, else
// in the quotes the shell that runs it reads. ssh runs the command with sh on Unix and with
// cmd.exe on Windows. A % is doubled, since ssh reads %h and the like in the line.
func proxyProgram(path, goos string) string {
	path = strings.ReplaceAll(path, "%", "%%")
	if goos == "windows" {
		// cmd.exe reads quotes as they are. A path cannot hold a quote there.
		if strings.ContainsAny(path, " \t&()^") {
			return `"` + path + `"`
		}
		return path
	}
	if !strings.ContainsAny(path, " \t\"'\\$`&|;<>()*?[]{}!#~^") {
		return path
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(path) + `"`
}

// sshBlock is the text of the block, with eol after each line.
func sshBlock(app, host, cloudflared, goos, eol string) string {
	lines := []string{
		sshBlockBegin(app),
		"Host " + host,
		"  ProxyCommand " + proxyProgram(cloudflared, goos) + " access ssh --hostname %h",
		"  StrictHostKeyChecking accept-new",
		"Match all",
		sshBlockEnd(app),
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

// cutSSHBlock takes the block of app out of content. It returns the rest and whether the block
// was there. A block that starts and does not end, or ends twice, is an error: the file is the
// person's and tifa does not guess what to cut.
func cutSSHBlock(content, app string) (rest string, found bool, err error) {
	start, end := sshBlockStart(app), sshBlockEnd(app)
	var out strings.Builder
	inBlock := false
	for len(content) > 0 {
		line, next, _ := strings.Cut(content, "\n")
		whole := content[:len(content)-len(next)]
		if next == "" && !strings.HasSuffix(whole, "\n") {
			whole = line // the last line has no ending
		}
		trimmed := strings.TrimRight(line, "\r")
		switch {
		case !inBlock && strings.HasPrefix(trimmed, start):
			if found {
				return "", false, fmt.Errorf("ssh config has the block of tifa %s twice: delete one of them", app)
			}
			inBlock, found = true, true
		case inBlock && trimmed == end:
			inBlock = false
		case inBlock:
		default:
			out.WriteString(whole)
		}
		content = next
	}
	if inBlock {
		return "", false, fmt.Errorf("ssh config has a block that starts with %q and never ends with %q: delete it, and run setup again", start, end)
	}
	return out.String(), found, nil
}

// withSSHBlock returns content with the block of app first and nothing else changed.
func withSSHBlock(content, app, host, cloudflared, goos string) (string, error) {
	rest, _, err := cutSSHBlock(content, app)
	if err != nil {
		return "", err
	}
	return sshBlock(app, host, cloudflared, goos, fileEOL(content, goos)) + rest, nil
}

// withoutSSHBlock returns content without the block of app, and whether it was there.
func withoutSSHBlock(content, app string) (string, bool, error) {
	return cutSSHBlock(content, app)
}

// readSSHConfig reads the file, or "" when there is none.
func readSSHConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// sshBlockState says what the file has of the block of app: whether the file holds exactly the
// text withSSHBlock would give, and whether it holds a block at all.
func sshBlockState(path, app, host, cloudflared string) (same, present bool, err error) {
	content, err := readSSHConfig(path)
	if err != nil {
		return false, false, err
	}
	_, present, err = cutSSHBlock(content, app)
	if err != nil {
		return false, false, err
	}
	want, err := withSSHBlock(content, app, host, cloudflared, toolOS)
	return want == content, present, err
}

// writeSSHBlock puts the block of app at the top of the file at path. A file that is not there is
// made with mode 0600, and the folder with 0700. A file that is there keeps its mode, and is
// written in place, so a link to a file of a dotfiles folder stays a link.
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

// removeSSHBlock deletes the block of app from the file, and says whether it was there.
func removeSSHBlock(path, app string) (bool, error) {
	content, err := readSSHConfig(path)
	if err != nil {
		return false, err
	}
	rest, found, err := withoutSSHBlock(content, app)
	if err != nil || !found {
		return false, err
	}
	return true, os.WriteFile(path, []byte(rest), 0o600)
}

// hasSSHBlock says whether the file at path holds the block of app.
func hasSSHBlock(path, app string) (bool, error) {
	content, err := readSSHConfig(path)
	if err != nil {
		return false, err
	}
	_, found, err := cutSSHBlock(content, app)
	return found, err
}
