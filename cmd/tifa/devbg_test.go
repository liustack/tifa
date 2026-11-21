package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/deverrors"
	"github.com/spf13/pflag"
)

// startSleeper starts this test binary as a program that sleeps a minute, unless the test ends it.
func startSleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), sleeperEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

// fakeStatusDev holds the lock of a new app as tifa dev does, with the address of a server that
// answers state at deverrors.StatusPath in the name of this process, and returns the app root.
func fakeStatusDev(t *testing.T, status string) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != deverrors.StatusPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set(deverrors.PIDHeader, strconv.Itoa(os.Getpid()))
		_, _ = w.Write([]byte(status))
	}))
	t.Cleanup(srv.Close)
	root := t.TempDir()
	l, err := lockDev(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	release := func() { l.Close() }
	if err := l.SetLog(logPath(root)); err != nil {
		t.Fatal(err)
	}
	if err := l.SetURL(srv.URL); err != nil {
		t.Fatal(err)
	}
	return root, release
}

// Every flag the user gives, and none the user did not give, reaches the tifa dev that
// --background starts. A new flag of tifa dev is covered by the loop over all of them.
func TestBackgroundForwardsEveryFlag(t *testing.T) {
	cmd := newDevCmd()
	if err := cmd.ParseFlags([]string{"--dir", "my app", "--addr", "0.0.0.0:8000", "--background"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"dev", "--addr=0.0.0.0:8000", "--dir=my app"}
	if got := forwardArgs(cmd); !slices.Equal(got, want) {
		t.Fatalf("forwardArgs = %q, want %q", got, want)
	}

	// Set each flag but --background to a value that is not its default, and read them back from
	// the command line forwardArgs makes.
	all := newDevCmd()
	sample := map[string]string{"bool": "true", "string": "sample value", "int": "7", "duration": "3s"}
	var args []string
	all.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "background" || f.Name == "help" {
			return
		}
		v, ok := sample[f.Value.Type()]
		if !ok {
			t.Fatalf("flag --%s has the type %s, which this test has no sample for", f.Name, f.Value.Type())
		}
		args = append(args, "--"+f.Name+"="+v)
	})
	if err := all.ParseFlags(append(args, "--background")); err != nil {
		t.Fatal(err)
	}
	back := newDevCmd()
	if err := back.ParseFlags(forwardArgs(all)[1:]); err != nil {
		t.Fatal(err)
	}
	all.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "background" {
			if back.Flags().Lookup("background").Changed {
				t.Errorf("--background reaches the child, which would start another one")
			}
			return
		}
		got := back.Flags().Lookup(f.Name)
		if got.Changed != f.Changed || got.Value.String() != f.Value.String() {
			t.Errorf("--%s: the child has %q (set: %v), the command had %q (set: %v)", f.Name, got.Value, got.Changed, f.Value, f.Changed)
		}
	})
}

func TestStatusOfARunningDev(t *testing.T) {
	root, _ := fakeStatusDev(t, `{"state":"ok"}`)
	out, _, err := run(t, "dev", "status", "--dir", root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tifa dev is running", "url:     http://127.0.0.1:", "pid:     " + strconv.Itoa(os.Getpid()), "started: ", "build:   ok\n", "log:     " + logPath(root)} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

func TestStatusJSON(t *testing.T) {
	root, _ := fakeStatusDev(t, `{"state":"failed","failure":{"title":"Build failed","error":"\n\u001b[31mgo build: app/store.go:3:2: undefined: x\u001b[39m\nexit status 1"}}`)
	out, _, err := run(t, "dev", "status", "--json", "--dir", root)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Running bool   `json:"running"`
		PID     int    `json:"pid"`
		URL     string `json:"url"`
		Started string `json:"started"`
		Build   struct{ State, Error string }
		Log     string `json:"log"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !got.Running || got.PID != os.Getpid() || !strings.HasPrefix(got.URL, "http://127.0.0.1:") || got.Log != logPath(root) {
		t.Errorf("status = %+v", got)
	}
	if _, err := time.Parse(time.RFC3339, got.Started); err != nil {
		t.Errorf("started %q: %v", got.Started, err)
	}
	if got.Build.State != "failed" || got.Build.Error != "go build: app/store.go:3:2: undefined: x" {
		t.Errorf("build = %+v, want failed with the first line of the error, without color codes", got.Build)
	}
}

// What the lock does not hold is not running, whatever a file says: no holder, a record of a process
// that lives, a record of this very process.
func TestStatusAndStopTrustTheLockNotTheRecord(t *testing.T) {
	bystander := startSleeper(t)
	answers := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(deverrors.PIDHeader, strconv.Itoa(bystander.Process.Pid))
		_, _ = w.Write([]byte(`{"state":"ok"}`))
	}))
	t.Cleanup(answers.Close)

	for name, pid := range map[string]int{"a process that lives": bystander.Process.Pid, "this process": os.Getpid()} {
		root := t.TempDir()
		data, _ := json.Marshal(lockHolder{PID: pid, Kind: kindDev, URL: answers.URL, Started: time.Now()})
		writeFile(t, filepath.Join(root, codegen.Dir, codegen.LockFile), string(data))

		out, _, err := run(t, "dev", "status", "--dir", root)
		if exitCodeOf(err) != 1 || !strings.Contains(out, "not running") {
			t.Errorf("%s, status: exit %d, %v, %q", name, exitCodeOf(err), err, out)
		}
		out, _, err = run(t, "dev", "status", "--json", "--dir", root)
		if exitCodeOf(err) != 1 || strings.TrimSpace(out) != "{\n  \"running\": false\n}" {
			t.Errorf("%s, status --json: exit %d, %v, %q", name, exitCodeOf(err), err, out)
		}
		// Had stop signalled this process, it would be gone, or the test would be.
		out, _, err = run(t, "dev", "stop", "--dir", root)
		if err != nil || !strings.Contains(out, "not running") {
			t.Errorf("%s, stop: %v, %q", name, err, out)
		}
		if !processAlive(bystander.Process.Pid) {
			t.Errorf("%s: the other process was stopped", name)
		}
	}
	// No file at all, and no .tifa made by asking.
	root := t.TempDir()
	if _, _, err := run(t, "dev", "status", "--dir", root); exitCodeOf(err) != 1 {
		t.Errorf("no file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, codegen.Dir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("asking made .tifa: %v", err)
	}
}

// A process holds the lock, and the address of the record does not answer as that tifa dev. Stop
// does not signal the pid of the record: it may be a record the new holder has not replaced.
func TestStopDoesNotSignalWhatDoesNotAnswerAsTifaDev(t *testing.T) {
	was := startWait
	startWait = 300 * time.Millisecond
	t.Cleanup(func() { startWait = was })
	bystander := startSleeper(t)

	root := t.TempDir()
	l, err := lockDev(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	for name, url := range map[string]string{"no address yet": "", "nothing answers": closed.URL} {
		if url == "" {
			l.write(lockHolder{PID: bystander.Process.Pid, Kind: kindDev})
		} else {
			l.write(lockHolder{PID: bystander.Process.Pid, Kind: kindDev, URL: url})
		}
		_, _, err := run(t, "dev", "stop", "--dir", root)
		if err == nil || !strings.Contains(err.Error(), "Nothing was signalled") {
			t.Errorf("%s: %v", name, err)
		}
		if !processAlive(bystander.Process.Pid) {
			t.Errorf("%s: the process of the record was stopped", name)
		}
	}
}

func TestStopLeavesTifaBuildAlone(t *testing.T) {
	root := t.TempDir()
	l, err := lockApp(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	_, _, err = run(t, "dev", "stop", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "tifa build holds the lock") {
		t.Fatalf("stop while a build runs: %v", err)
	}
	out, _, err := run(t, "dev", "status", "--dir", root)
	if exitCodeOf(err) != 1 || !strings.Contains(out, "tifa build holds its lock") {
		t.Fatalf("status while a build runs: %v, %q", err, out)
	}
}

// Stop signals the tifa dev that answers for the lock, and returns once the lock is free.
func TestStopSignalsTheAnsweringDevAndWaitsForTheLock(t *testing.T) {
	// A program that holds the lock the way tifa dev does, until it is asked to stop as tifa dev is,
	// and records itself. The lock needs a process of its own to be asked.
	if os.Getenv("TIFA_TEST_HELPER_HOLDER") == "1" {
		root := os.Getenv("TIFA_TEST_HELPER_ROOT")
		l, err := lockDev(root)
		if err != nil {
			os.Exit(3)
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(deverrors.PIDHeader, strconv.Itoa(os.Getpid()))
			_, _ = w.Write([]byte(`{"state":"ok"}`))
		}))
		if err := l.SetURL(srv.URL); err != nil {
			os.Exit(3)
		}
		ctx, stop, err := stopContext(context.Background())
		if err != nil {
			os.Exit(3)
		}
		defer stop()
		<-ctx.Done()
		time.Sleep(300 * time.Millisecond) // a shutdown that takes a moment
		srv.Close()
		l.Close()
		os.Exit(0)
	}
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := exec.Command(exe, "-test.run=^TestStopSignalsTheAnsweringDevAndWaitsForTheLock$")
	helper.Env = append(os.Environ(), "TIFA_TEST_HELPER_HOLDER=1", "TIFA_TEST_HELPER_ROOT="+root)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- helper.Wait() }()
	t.Cleanup(func() { helper.Process.Kill() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, held, _ := lockHeld(root); held {
			if rec, _ := devRecord(root); rec.URL != "" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper never held the lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	out, _, err := run(t, "dev", "stop", "--dir", root)
	if err != nil || !strings.Contains(out, "tifa dev stopped (pid "+strconv.Itoa(helper.Process.Pid)+")") {
		t.Fatalf("stop: %v, %q", err, out)
	}
	if _, held, _ := lockHeld(root); held {
		t.Fatal("the lock is held after stop returned")
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("the helper: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the helper is still running")
	}
}

// A second tifa dev, in the foreground or with --background, fails and says how to look at the one
// that runs and how to stop it.
func TestSecondDevSaysHowToStatusAndStop(t *testing.T) {
	root, _ := fakeStatusDev(t, `{"state":"ok"}`)
	for _, args := range [][]string{{"dev", "--dir", root}, {"dev", "--background", "--dir", root}} {
		_, stderr, err := run(t, args...)
		if err == nil || !strings.HasPrefix(err.Error(), "tifa dev is already running in this app at http://127.0.0.1:") ||
			!strings.Contains(err.Error(), "(pid "+strconv.Itoa(os.Getpid())+")") ||
			!strings.Contains(err.Error(), "`go tool tifa dev status --dir "+root+"`") ||
			!strings.Contains(err.Error(), "`go tool tifa dev stop --dir "+root+"`") {
			t.Errorf("%v: %v", args, err)
		}
		if stderr != "" {
			t.Errorf("%v: stderr %q", args, stderr)
		}
	}
}

func TestLogsPrintsTheTail(t *testing.T) {
	root := t.TempDir()
	var log strings.Builder
	for i := 1; i <= 250; i++ {
		log.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	writeFile(t, logPath(root), log.String())

	out, _, err := run(t, "dev", "logs", "--dir", root)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 100 || lines[0] != "line 151" || lines[99] != "line 250" {
		t.Errorf("default: %d lines from %q to %q", len(lines), lines[0], lines[len(lines)-1])
	}
	out, _, _ = run(t, "dev", "logs", "-n", "2", "--dir", root)
	if out != "line 249\nline 250\n" {
		t.Errorf("-n 2: %q", out)
	}
	out, _, _ = run(t, "dev", "logs", "-n", "0", "--dir", root)
	if out != "" {
		t.Errorf("-n 0: %q", out)
	}
	if _, _, err := run(t, "dev", "logs", "-n", "-1", "--dir", root); err == nil {
		t.Error("-n -1 is accepted")
	}
	// A log whose last line has no newline yet.
	writeFile(t, logPath(root), "a\nb")
	if out, _, _ = run(t, "dev", "logs", "--dir", root); out != "a\nb\n" {
		t.Errorf("unfinished line: %q", out)
	}
}

func TestLogsWithoutALog(t *testing.T) {
	_, _, err := run(t, "dev", "logs", "--dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no log") || !strings.Contains(err.Error(), "--background") {
		t.Fatalf("no log: %v", err)
	}
	// A tifa dev that prints to its terminal keeps none.
	root := t.TempDir()
	l, err := lockDev(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	if err := l.SetURL("http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, logPath(root), "an old log\n")
	_, _, err = run(t, "dev", "logs", "--dir", root)
	if err == nil || !strings.Contains(err.Error(), "prints to its terminal") {
		t.Fatalf("foreground: %v", err)
	}
}

// Follow prints what is added, also after the log was cut short for a new run, and ends once no
// tifa dev holds the lock.
func TestLogsFollowEndsWithTheDev(t *testing.T) {
	root, releaseFakeDev := fakeStatusDev(t, `{"state":"ok"}`)
	writeFile(t, logPath(root), "first\n")
	var out safeBuffer
	done := make(chan error, 1)
	go func() {
		cmd := newRootCmd()
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"dev", "logs", "-f", "-n", "1", "--dir", root})
		done <- cmd.Execute()
	}()
	waitFor(t, func() bool { return out.String() == "first\n" })
	appendTo(t, logPath(root), "second\n")
	waitFor(t, func() bool { return out.String() == "first\nsecond\n" })
	// A new run begins the log again.
	writeFile(t, logPath(root), "new\n")
	waitFor(t, func() bool { return out.String() == "first\nsecond\nnew\n" })
	select {
	case err := <-done:
		t.Fatalf("follow ended while the lock is held: %v", err)
	default:
	}
	// The holder ends after a last line.
	appendTo(t, logPath(root), "bye\n")
	releaseFakeDev()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("follow did not end when the lock was freed")
	}
	if got := out.String(); got != "first\nsecond\nnew\nbye\n" {
		t.Errorf("output %q", got)
	}
}

// safeBuffer is a buffer that a command writes while the test reads it.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("the condition did not come true within 10s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestLockHeldFollowsTheLock(t *testing.T) {
	root := t.TempDir()
	if _, held, err := lockHeld(root); held || err != nil {
		t.Fatalf("no file: %v, %v", held, err)
	}
	l, err := lockDev(root)
	if err != nil {
		t.Fatal(err)
	}
	holder, held, err := lockHeld(root)
	if !held || err != nil || holder.PID != os.Getpid() || holder.Kind != kindDev || holder.Started.IsZero() || !holder.isDev() {
		t.Fatalf("held: %+v, %v, %v", holder, held, err)
	}
	l.Close()
	if holder, held, _ = lockHeld(root); held {
		t.Fatalf("held after Close, record %+v", holder)
	}
}

// Looking at the lock takes it for an instant. A tifa dev or tifa build that starts then, or
// another look, must not take it for a holder.
func TestLockHeldDoesNotBlockOthers(t *testing.T) {
	root := t.TempDir()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				lockHeld(root) // it finds the lock held now and then: the loop below holds it
			}
		}
	}()
	for range 200 {
		l, err := lockDev(root)
		if err != nil {
			t.Fatalf("a start while a look runs: %v", err)
		}
		l.Close()
	}
	close(stop)
	<-done
}
