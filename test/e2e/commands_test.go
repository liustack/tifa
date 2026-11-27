package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/testproc"
)

// The commands that OnCommand of test/apps/api adds run in the built program: with their flags,
// the option of the app from .env, the environment and a flag, no OnStart, an error printed once
// with exit status 1, and a context that SIGINT ends. A second SIGINT ends a command that keeps
// going.
func TestAppCommands(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("the test stops the commands with SIGINT")
	}
	exe := buildWith(t, filepath.Join(repoRoot(t), "test", "apps", "api"), withoutNode(t))
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, ".env"), []byte("HELLO_GREETING=hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// command runs exe in work, where .env is, and returns its output, its error output and its
	// exit status.
	command := func(env []string, args ...string) (stdout, stderr string, code int) {
		t.Helper()
		cmd := exec.Command(exe, args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), env...)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		var exit *exec.ExitError
		switch {
		case errors.As(err, &exit):
			code = exit.ExitCode()
		case err != nil:
			t.Fatalf("%q: %v", args, err)
		}
		return out.String(), errOut.String(), code
	}

	for _, c := range []struct {
		env  []string
		args []string
		want string
	}{
		{nil, []string{"hello", "Ann"}, "hi, Ann\n"},
		{nil, []string{"hello", "Ann", "--loud"}, "hi, Ann!\n"},
		{[]string{"HELLO_GREETING=hey"}, []string{"hello", "Ann"}, "hey, Ann\n"},
		{[]string{"HELLO_GREETING=hey"}, []string{"hello", "--hello-greeting", "yo", "Ann"}, "yo, Ann\n"},
	} {
		out, errOut, code := command(c.env, c.args...)
		if out != c.want || errOut != "" || code != 0 {
			t.Errorf("%q with %q: %q, %q, exit %d, want %q", c.args, c.env, out, errOut, code, c.want)
		}
	}

	out, errOut, code := command(nil, "fail")
	if out != "" || errOut != "no such note\n" || code != 1 {
		t.Errorf("fail: %q, %q, exit %d, want the error once and exit 1", out, errOut, code)
	}
	// The settings are loaded before the command runs.
	if out, errOut, code := command(nil, "hello", "Ann", "--ssr-vms", "two"); out != "" || !strings.Contains(errOut, "flag --ssr-vms") || code != 1 {
		t.Errorf("an invalid setting: %q, %q, exit %d", out, errOut, code)
	}
	out, _, _ = command(nil, "--help")
	for _, want := range []string{"hello       Greet someone", "wait        Wait for Ctrl-C", "fail        Fail", "serve       Serve the application (default)"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q:\n%s", want, out)
		}
	}

	// startWait starts the command wait and returns once it waits. ended closes when it has ended,
	// and *err is then what Wait returned.
	startWait := func(args ...string) (cmd *exec.Cmd, output *lockedBuffer, ended chan struct{}, err *error) {
		t.Helper()
		output = &lockedBuffer{}
		cmd = exec.Command(exe, append([]string{"wait"}, args...)...)
		cmd.Dir = work
		cmd.Stdout, cmd.Stderr = output, output
		if err := testproc.Start(cmd); err != nil {
			t.Fatal(err)
		}
		ended, err = make(chan struct{}), new(error)
		go func() {
			*err = cmd.Wait()
			close(ended)
		}()
		t.Cleanup(func() {
			select {
			case <-ended:
			default:
				cmd.Process.Kill()
				<-ended
			}
		})
		output.waitFor(t, func(s string) bool { return strings.Contains(s, "waiting\n") })
		return cmd, output, ended, err
	}
	wait := func(ended chan struct{}, err *error, output *lockedBuffer) error {
		t.Helper()
		select {
		case <-ended:
			return *err
		case <-time.After(15 * time.Second):
			t.Fatalf("the command did not end:\n%s", output)
			return nil
		}
	}

	cmd, output, ended, waitErr := startWait()
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if err := wait(ended, waitErr, output); err != nil || output.String() != "waiting\nstopping\n" {
		t.Errorf("wait after SIGINT: %v\n%s", err, output)
	}

	cmd, output, ended, waitErr = startWait("--hang")
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	output.waitFor(t, func(s string) bool { return strings.Contains(s, "stopping\n") })
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	err := wait(ended, waitErr, output)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGINT {
		t.Errorf("wait --hang after two SIGINT: %v, want an end by SIGINT\n%s", err, output)
	}
	if strings.Contains(output.String(), "apitest:") {
		t.Errorf("a hook ran:\n%s", output)
	}
}
