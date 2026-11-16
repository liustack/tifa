package main

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/deverrors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The waits of the commands below.
const (
	// readyWait is how long tifa dev --background waits for the server to answer. The first build of
	// an app can take a minute, see errorsWait.
	readyWait = 5 * time.Minute
	// stopWait is how long tifa dev stop waits for the lock to be free. The app may take
	// shutdown_timeout for requests, again for handlers and once more for OnShutdown.
	stopWait = 60 * time.Second
	// askWait bounds one question to the status address of tifa dev.
	askWait = 5 * time.Second
)

// startWait is how long tifa dev stop gives a tifa dev that just started to answer. A variable
// for the tests.
var startWait = 15 * time.Second

// errDevSystem says that tifa dev does not run here.
var errDevSystem = errors.New("tifa dev does not run on Windows yet, use WSL")

func checkDevSystem() error {
	if runtime.GOOS == "windows" {
		return errDevSystem
	}
	return nil
}

// devCommand writes a tifa dev command as an agent runs it, for the hints in messages. dir is the
// --dir the user gave: the hint keeps it, since without it the command looks at the current folder.
func devCommand(dir string, args ...string) string {
	line := "go tool tifa dev " + strings.Join(args, " ")
	if dir != "." && dir != "" {
		line += " --dir " + dir
	}
	return strings.TrimSpace(line)
}

// seeStopHint is what a refusal to start a second tifa dev adds: how to look at the one that runs.
func seeStopHint(dir string) string {
	return fmt.Sprintf("See it with `%s`, stop it with `%s`", devCommand(dir, "status"), devCommand(dir, "stop"))
}

func logPath(root string) string { return filepath.Join(root, codegen.Dir, codegen.LogFile) }

// stderrIsLog says whether the standard error of this process is the log file of the app, which is
// so for a tifa dev that tifa dev --background started.
func stderrIsLog(root string) bool {
	logInfo, err := os.Stat(logPath(root))
	if err != nil {
		return false
	}
	errInfo, err := os.Stderr.Stat()
	return err == nil && os.SameFile(logInfo, errInfo)
}

// forwardArgs is the command line of the tifa dev that --background starts: every flag the user
// gave, as given, except --background. The flags are read from what the command parsed, so a flag
// added later is forwarded without a change here.
func forwardArgs(cmd *cobra.Command) []string {
	args := []string{"dev"}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if f.Name == "background" {
			return
		}
		if v, ok := f.Value.(pflag.SliceValue); ok {
			for _, item := range v.GetSlice() {
				args = append(args, "--"+f.Name+"="+item)
			}
			return
		}
		args = append(args, "--"+f.Name+"="+f.Value.String())
	})
	return args
}

// askStatus gets the state of the builds from the tifa dev that holder names. It also proves that
// the process of holder is that tifa dev: the answer must name its process ID. An address where
// nothing answers, or a program that is not it, is an error.
func askStatus(holder lockHolder) (deverrors.Status, error) {
	var st deverrors.Status
	if holder.URL == "" {
		return st, errors.New("it has not recorded an address yet")
	}
	client := &http.Client{Timeout: askWait}
	resp, err := client.Get(holder.URL + deverrors.StatusPath)
	if err != nil {
		return st, fmt.Errorf("nothing answers at %s: %v", holder.URL, err)
	}
	defer resp.Body.Close()
	if resp.Header.Get(deverrors.PIDHeader) != strconv.Itoa(holder.PID) {
		return st, fmt.Errorf("the program at %s is not the tifa dev of .tifa/dev.lock", holder.URL)
	}
	if resp.StatusCode != http.StatusOK {
		return st, fmt.Errorf("tifa dev at %s answered %s", holder.URL, resp.Status)
	}
	if err := json.UnmarshalRead(resp.Body, &st); err != nil {
		return st, fmt.Errorf("tifa dev at %s answered no state: %w", holder.URL, err)
	}
	return st, nil
}

// firstLine gives the first line of an error text that says something, without color codes.
func firstLine(text string) string {
	for line := range strings.SplitSeq(ansiColor.ReplaceAllString(text, ""), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// runBackground starts tifa dev detached from the terminal and returns once the app answers.
func runBackground(cmd *cobra.Command, dir, root string) error {
	if err := checkDevSystem(); err != nil {
		return err
	}
	holder, held, err := lockHeld(root)
	if err != nil {
		return err
	}
	if held {
		return busyDev(dir, holder)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, codegen.Dir), 0o755); err != nil {
		return err
	}
	// The log starts over with each tifa dev. It is opened with O_APPEND, so what a follower
	// reads of it stays in order when the file is cut short.
	logFile, err := os.OpenFile(logPath(root), os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	child := exec.Command(exe, forwardArgs(cmd)...)
	child.Stdout, child.Stderr = logFile, logFile // Stdin stays nil: the null device
	if err := detach(child); err != nil {
		return err
	}
	// The child gets only the files named for it, not what this process inherited, such as the lock
	// a script holds with flock.
	if err := closeInheritedOnExec(); err != nil {
		return fmt.Errorf("the server would inherit the files this command inherited: %w", err)
	}
	if err := child.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	ctx, stop := signal.NotifyContext(cmd.Context(), stopSignals()...)
	defer stop()
	url, err := waitReady(ctx, root, child.Process.Pid, exited)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		// The one who started it does not want it any more.
		_ = child.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(stopWait):
		}
		return errors.New("interrupted, the server was stopped")
	default:
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "tifa dev: ready at %s (pid %d)\n", url, child.Process.Pid)
	fmt.Fprintf(out, "  log:    %s\n", devCommand(dir, "logs"))
	fmt.Fprintf(out, "  status: %s\n", devCommand(dir, "status"))
	fmt.Fprintf(out, "  stop:   %s\n", devCommand(dir, "stop"))
	return nil
}

// waitReady waits until the tifa dev with process ID pid has built the app and the app answers at
// its address, and returns the address. It fails when that process ends first, when its build
// failed (it keeps running then, and rebuilds when a file changes), or after readyWait.
func waitReady(ctx context.Context, root string, pid int, exited <-chan error) (string, error) {
	deadline := time.NewTimer(readyWait)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	client := &http.Client{Timeout: askWait, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case err := <-exited:
			return "", fmt.Errorf("tifa dev stopped before it was ready%s.\n%s", exitReason(err), tailText(logPath(root), 20))
		case <-deadline.C:
			return "", fmt.Errorf("tifa dev (pid %d) did not answer within %s. It still runs, see its log with `go tool tifa dev logs`", pid, readyWait)
		case <-tick.C:
		}
		// The record may still be the one before this tifa dev's, so it counts only with its pid.
		holder, err := devRecord(root)
		if err != nil || holder.PID != pid || holder.URL == "" {
			continue
		}
		st, err := askStatus(holder)
		if err != nil {
			continue
		}
		switch st.State {
		case deverrors.Failed:
			return "", fmt.Errorf("the first build failed: %s\nThe server still runs at %s and builds again when you save a file. `go tool tifa errors` prints the errors, `go tool tifa dev stop` stops it.\n%s",
				firstLine(st.Failure.Error), holder.URL, cut(ansiColor.ReplaceAllString(st.Failure.Error, ""), 20))
		case deverrors.OK:
			resp, err := client.Get(holder.URL + "/")
			if err != nil {
				continue
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 500 {
				return "", fmt.Errorf("the app answered %s for GET / at %s. The server still runs, see `go tool tifa errors` and `go tool tifa dev logs`", resp.Status, holder.URL)
			}
			return holder.URL, nil
		}
	}
}

func exitReason(err error) string {
	if err == nil {
		return ""
	}
	return " (" + err.Error() + ")"
}

// cut keeps the first n lines of text.
func cut(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = append(lines[:n], fmt.Sprintf("... %d more lines", len(lines)-n))
	}
	return strings.Join(lines, "\n")
}

// busyDev is the error of a tifa dev that finds the lock of the app taken.
func busyDev(dir string, holder lockHolder) error {
	busy := &busyError{holder}
	if holder.isDev() {
		return errors.New("tifa dev is already running in this app " + busy.Detail() + ". " + seeStopHint(dir))
	}
	return busy
}

// tailText gives the last n lines of the file at path, or what went wrong with reading it.
func tailText(path string, n int) string {
	text, err := tailLines(path, n)
	if err != nil {
		return "(no log: " + err.Error() + ")"
	}
	return strings.TrimRight(text, "\n")
}

// maxTail is how much of the end of the log tifa dev logs reads.
const maxTail = 8 << 20

// tailLines gives the last n lines of the file at path, and the offset of its end.
func tailLines(path string, n int) (string, error) {
	text, _, err := tailFrom(path, n)
	return text, err
}

func tailFrom(path string, n int) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	size := info.Size()
	start := max(0, size-maxTail)
	data := make([]byte, size-start)
	if _, err := f.ReadAt(data, start); err != nil && !errors.Is(err, io.EOF) {
		return "", 0, err
	}
	if start > 0 {
		// The first line is cut off.
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	data = bytes.TrimSuffix(data, []byte("\n"))
	lines := bytes.Split(data, []byte("\n"))
	if len(data) == 0 {
		lines = nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 0 {
		return "", size, nil
	}
	return string(bytes.Join(lines, []byte("\n"))) + "\n", size, nil
}

// devStatus is what tifa dev status prints with --json.
type devStatus struct {
	Running bool       `json:"running"`
	PID     int        `json:"pid,omitzero"`
	URL     string     `json:"url,omitempty"`
	Started *time.Time `json:"started,omitempty"`
	Build   *buildInfo `json:"build,omitempty"`
	Log     string     `json:"log,omitempty"`
}

// buildInfo is the state of the last build: starting, building, ok, failed (with the first line of
// its error), or unknown when tifa dev did not answer.
type buildInfo struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

func newDevStatusCmd(dir *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Say whether a tifa dev runs in the app",
		Long: `status says whether a tifa dev runs in the application, at which address, with which process
ID, since when, and whether its last build worked (ok, or failed with the first line of the
error). A tifa dev runs when it holds the lock .tifa/dev.lock: a record left by one that ended does
not count.

It exits with status 0 when a tifa dev runs and 1 when none does.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkDevSystem(); err != nil {
				return err
			}
			root, err := filepath.Abs(*dir)
			if err != nil {
				return err
			}
			holder, held, err := lockHeld(root)
			if err != nil {
				return err
			}
			var s devStatus
			if held && holder.isDev() {
				s = devStatus{Running: true, PID: holder.PID, URL: holder.URL, Log: holder.Log}
				if !holder.Started.IsZero() {
					s.Started = &holder.Started
				}
				s.Build = &buildInfo{State: "starting"}
				if holder.URL != "" {
					st, err := askStatus(holder)
					if err != nil {
						s.Build = &buildInfo{State: "unknown", Error: err.Error()}
					} else {
						s.Build = &buildInfo{State: st.State}
						if st.Failure != nil {
							s.Build.Error = firstLine(st.Failure.Error)
						}
					}
				}
			}
			out := cmd.OutOrStdout()
			if asJSON {
				data, err := json.Marshal(s, jsontext.WithIndent("  "))
				if err != nil {
					return err
				}
				if _, err := out.Write(append(data, '\n')); err != nil {
					return err
				}
			} else {
				io.WriteString(out, statusText(s, held && !holder.isDev()))
			}
			if !s.Running {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the state as JSON")
	return cmd
}

func statusText(s devStatus, buildRuns bool) string {
	if !s.Running {
		if buildRuns {
			return "tifa dev is not running in this app (tifa build holds its lock)\n"
		}
		return "tifa dev is not running in this app\n"
	}
	var b strings.Builder
	b.WriteString("tifa dev is running in this app\n")
	if s.URL != "" {
		fmt.Fprintf(&b, "  url:     %s\n", s.URL)
	}
	fmt.Fprintf(&b, "  pid:     %d\n", s.PID)
	if s.Started != nil {
		fmt.Fprintf(&b, "  started: %s (%s ago)\n", s.Started.Local().Format("2006-01-02 15:04:05"), time.Since(*s.Started).Round(time.Second))
	}
	build := s.Build.State
	if s.Build.Error != "" {
		build += ": " + s.Build.Error
	}
	fmt.Fprintf(&b, "  build:   %s\n", build)
	if s.Log != "" {
		fmt.Fprintf(&b, "  log:     %s\n", s.Log)
	} else {
		b.WriteString("  log:     none, it prints to its terminal\n")
	}
	return b.String()
}

func newDevLogsCmd(dir *string) *cobra.Command {
	var lines int
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print the log of a tifa dev started with --background",
		Long: `logs prints the last lines of .tifa/dev.log, where a tifa dev started with --background writes
everything it prints: its own log lines, the output of the app and Vite's warnings. With -f it then
keeps printing new lines until the tifa dev ends. A tifa dev that ended leaves its log, so this also
shows why it stopped. A tifa dev started without --background prints to its terminal and keeps no log.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkDevSystem(); err != nil {
				return err
			}
			if lines < 0 {
				return errors.New("-n must not be negative")
			}
			root, err := filepath.Abs(*dir)
			if err != nil {
				return err
			}
			holder, held, err := lockHeld(root)
			if err != nil {
				return err
			}
			// A record that has an address has decided where the output goes.
			if held && holder.isDev() && holder.URL != "" && holder.Log == "" {
				return errors.New("this tifa dev prints to its terminal and keeps no log. Stop it and start it with --background: " + devCommand(*dir, "stop") + ", then " + devCommand(*dir, "--background"))
			}
			text, size, err := tailFrom(logPath(root), lines)
			if errors.Is(err, os.ErrNotExist) {
				return errors.New("no log in this app. `" + devCommand(*dir, "--background") + "` starts a tifa dev that keeps one")
			}
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if _, err := io.WriteString(out, text); err != nil {
				return err
			}
			if !follow || !held {
				return nil
			}
			return followLog(cmd.Context(), out, root, size)
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "print this many of the last lines")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new lines until tifa dev ends")
	return cmd
}

// followLog prints what is added to the log after offset, until no tifa dev holds the lock any more
// and the rest is printed. A log that gets shorter is read again from its start: a new tifa dev
// began it again.
func followLog(ctx context.Context, out io.Writer, root string, offset int64) error {
	path := logPath(root)
	drain := func() error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.Size() < offset {
			offset = 0
		}
		if info.Size() == offset {
			return nil
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(f, info.Size()-offset))
		offset += n
		return err
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	checks := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		if err := drain(); err != nil {
			return err
		}
		// Whether the server still runs is asked about once a second: the look at the lock costs more.
		if checks++; checks%5 == 0 {
			if _, held, err := lockHeld(root); err != nil {
				return err
			} else if !held {
				return drain()
			}
		}
	}
}

func newDevStopCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the tifa dev of the app",
		Long: `stop stops the tifa dev that holds .tifa/dev.lock and waits until it is gone and the lock is free.
It signals a process only after that process, at the address the lock records, has answered as
the tifa dev of this app. A record left by a tifa dev that ended, with no process holding the lock,
signals nothing: it says that no tifa dev runs and exits with status 0.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkDevSystem(); err != nil {
				return err
			}
			root, err := filepath.Abs(*dir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			holder, running, err := findDev(root)
			if err != nil {
				return err
			}
			if !running {
				fmt.Fprintln(out, "tifa dev is not running in this app")
				return nil
			}
			proc, err := os.FindProcess(holder.PID)
			if err != nil {
				return err
			}
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("signalling tifa dev (pid %d): %w", holder.PID, err)
			}
			deadline := time.Now().Add(stopWait)
			for {
				_, held, err := lockHeld(root)
				if err != nil {
					return err
				}
				if !held {
					fmt.Fprintf(out, "tifa dev stopped (pid %d)\n", holder.PID)
					return nil
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("tifa dev (pid %d) still runs %s after it was asked to stop. It may be waiting for a request or OnShutdown: run `%s` to see", holder.PID, stopWait, devCommand(*dir, "status"))
				}
				time.Sleep(100 * time.Millisecond)
			}
		},
	}
}

// findDev finds the tifa dev of the app at root that stop may signal. It is one only when it holds
// the lock and has answered at its recorded address with its own process ID: the record alone
// proves nothing, as a record outlives its holder and a new holder may not have written its own
// yet. A tifa dev that has just started gets startWait to listen and answer. It reports false
// when no process holds the lock, and an error when one does that is no tifa dev of this app.
func findDev(root string) (lockHolder, bool, error) {
	deadline := time.Now().Add(startWait)
	var last error
	for {
		holder, held, err := lockHeld(root)
		if err != nil || !held {
			return holder, false, err
		}
		if holder.PID != 0 && !holder.isDev() && holder.Kind == kindBuild {
			return holder, false, fmt.Errorf("tifa build holds the lock of this app (pid %d), not tifa dev. Not stopping it", holder.PID)
		}
		if holder.PID > 0 && holder.URL != "" {
			if _, last = askStatus(holder); last == nil {
				return holder, true, nil
			}
		} else {
			last = errors.New("its record has no address yet")
		}
		if time.Now().After(deadline) {
			return holder, false, fmt.Errorf("a process holds the lock of this app (record: pid %d), but it did not answer as tifa dev: %v. Nothing was signalled", holder.PID, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
