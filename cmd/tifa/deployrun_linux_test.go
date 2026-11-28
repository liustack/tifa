package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sshdRun runs the script of deploy run for the app shop in home, as sshd runs a command without a
// terminal: under a process named sshd that waits for it, with stdin as its input. The program of
// the release is program.
func sshdRun(t *testing.T, program string, args []string, stdin io.Reader) *exec.Cmd {
	t.Helper()
	home := t.TempDir()
	for _, dir := range []string{"shop/shared", "shop/current"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(home, "shop/current/shop"), program)
	if err := os.Chmod(filepath.Join(home, "shop/current/shop"), 0o755); err != nil {
		t.Fatal(err)
	}
	sh, err := os.ReadFile("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	sshd := filepath.Join(t.TempDir(), "sshd")
	if err := os.WriteFile(sshd, sh, 0o755); err != nil {
		t.Fatal(err)
	}
	// The command after it keeps sshd from taking the place of the shell of the script.
	c := exec.Command(sshd, "-c", `/bin/sh -c "$1"; exit $?`, "sshd", runScript("shop", args, false))
	c.Env = append(os.Environ(), "HOME="+home)
	c.Stdin = stdin
	return c
}

// Without a terminal, the program of deploy run gets the input of ssh, and its exit status is that
// of ssh.
func TestDeployRunWatchedPassesInputAndStatus(t *testing.T) {
	c := sshdRun(t, `#!/bin/sh
read line
echo "read <$line> $*"
exit 7
`, []string{"one", "two words"}, strings.NewReader("hello\n"))
	out, err := c.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit: %v, want status 7", err)
	}
	if string(out) != "read <hello> one two words\n" {
		t.Errorf("output %q", out)
	}
}

// Without a terminal, when the connection goes away, sshd ends the session and sends the program
// nothing: deploy run ends it with SIGTERM.
func TestDeployRunEndsTheProgramWhenTheConnectionGoes(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "term")
	ready := filepath.Join(t.TempDir(), "ready")
	c := sshdRun(t, `#!/bin/sh
trap 'echo term > '`+shellQuote(mark)+`'; exit 0' TERM
echo $$ > `+shellQuote(ready)+`
while :; do sleep 0.1; done
`, nil, nil)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, fileAt(ready))
	// A program left running when the test fails ends with it.
	t.Cleanup(func() {
		if pid, err := os.ReadFile(ready); err == nil {
			_ = exec.Command("kill", "-KILL", strings.TrimSpace(string(pid))).Run()
		}
	})
	// Gone and reaped, as sshd reaps the process of a session.
	if err := c.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.Wait()
	waitFor(t, fileAt(mark))
}

// When the program ends, the script ends its watcher with the sleep the watcher waits in, so the
// session leaves no process behind.
func TestDeployRunLeavesNoProcess(t *testing.T) {
	bin := t.TempDir()
	pids := filepath.Join(t.TempDir(), "pids")
	writeFile(t, filepath.Join(bin, "sleep"), "#!/bin/sh\necho $$ >> "+shellQuote(pids)+"\nexec /bin/sleep \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "sleep"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The program ends once the watcher sleeps.
	c := sshdRun(t, `#!/bin/sh
while [ ! -s `+shellQuote(pids)+` ]; do /bin/sleep 0.01; done
`, nil, nil)
	c.Env = append(c.Env, "PATH="+bin+":"+os.Getenv("PATH"))
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	data, err := os.ReadFile(pids)
	if err != nil {
		t.Fatal(err)
	}
	for _, pid := range strings.Fields(string(data)) {
		stat, err := os.ReadFile("/proc/" + pid + "/stat")
		if err != nil {
			continue
		}
		// The state follows the name in parentheses. A zombie has ended.
		if f := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:])); len(f) > 0 && f[0] != "Z" {
			_ = exec.Command("kill", "-KILL", pid).Run()
			t.Errorf("sleep %s of the watcher still runs after the script ended", pid)
		}
	}
}

// fileAt says a file is at path.
func fileAt(path string) func() bool {
	return func() bool { _, err := os.Stat(path); return err == nil }
}
