//go:build unix

package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// process is a line of ps: a process, its parent and its command line.
type process struct {
	pid, ppid int
	command   string
}

func (p process) String() string {
	command := p.command
	if len(command) > 80 {
		command = command[:80] + "..."
	}
	return fmt.Sprintf("%d %s", p.pid, command)
}

// firstLine is the first line of s without the space around it. The command line of the driver
// of Vite holds a whole script, and ps may print it over many lines.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

// running reports whether the process p still runs. A pid that another program took over, or a
// process that exited and waits to be reaped, has another command line or none.
func (p process) running() bool {
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(p.pid)).Output()
	return err == nil && firstLine(string(out)) == p.command
}

// processTable lists all the processes of the system.
func processTable(t *testing.T) map[int]process {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	all := map[int]process{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// The lines after the first of a command line that spans many lines do not start with
		// two numbers.
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		command := strings.TrimSpace(line)
		for range 2 {
			_, command, _ = strings.Cut(command, " ")
			command = strings.TrimLeft(command, " ")
		}
		all[pid] = process{pid, ppid, command}
	}
	return all
}

// descendants returns the processes below pid, its children, their children and so on.
func descendants(all map[int]process, pid int) []process {
	var found []process
	for queue := []int{pid}; len(queue) > 0; queue = queue[1:] {
		for _, p := range all {
			if p.ppid == queue[0] {
				found = append(found, p)
				queue = append(queue, p.pid)
			}
		}
	}
	slices.SortFunc(found, func(a, b process) int { return a.pid - b.pid })
	return found
}

// withCommand returns the first of procs whose command line has the program name, the last
// element of the path of its first word.
func withCommand(procs []process, name string) (process, bool) {
	for _, p := range procs {
		if first, _, _ := strings.Cut(p.command, " "); filepath.Base(first) == name {
			return p, true
		}
	}
	return process{}, false
}

// stillRunning waits up to within for every one of procs to end, and returns those that did not.
func stillRunning(procs []process, within time.Duration) []process {
	var left []process
	for deadline := time.Now().Add(within); ; time.Sleep(100 * time.Millisecond) {
		left = left[:0]
		for _, p := range procs {
			if p.running() {
				left = append(left, p)
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			return left
		}
	}
}

// lsof returns the names that lsof prints for the files of the process pid, narrowed by args.
func lsof(t *testing.T, pid int, args ...string) []string {
	t.Helper()
	cmd := exec.Command("lsof", append([]string{"-nP", "-Fn", "-p", strconv.Itoa(pid)}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && stderr.Len() == 0 {
		err = nil // lsof exits with 1 when it finds nothing
	}
	if err != nil {
		t.Fatalf("lsof %v: %v\n%s", cmd.Args[1:], err, stderr.Bytes())
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if name, ok := strings.CutPrefix(line, "n"); ok {
			names = append(names, name)
		}
	}
	return names
}

// listenPorts returns the TCP ports the process pid listens on.
func listenPorts(t *testing.T, pid int) []string {
	t.Helper()
	var ports []string
	for _, name := range lsof(t, pid, "-a", "-iTCP", "-sTCP:LISTEN") {
		if i := strings.LastIndex(name, ":"); i >= 0 {
			ports = append(ports, name[i+1:])
		}
	}
	return ports
}

// reaches reports whether something accepts connections on the loopback address with this port.
func reaches(port string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// childrenOf lists the processes below tifa dev and checks that the app and the driver of Vite are
// among them. When the test ends it kills those that still run, so a program that does not stop
// on its own does not stay on the machine.
func childrenOf(t *testing.T, d *devRun) (app, driver process, kids []process) {
	t.Helper()
	kids = descendants(processTable(t), d.cmd.Process.Pid)
	t.Cleanup(func() {
		for _, p := range kids {
			if p.running() {
				syscall.Kill(p.pid, syscall.SIGKILL)
			}
		}
	})
	app, hasApp := withCommand(kids, "tifa-dev")
	driver, hasDriver := withCommand(kids, "node")
	if !hasApp || !hasDriver {
		t.Fatalf("tifa dev %d has these processes below it, want the app (tifa-dev) and the driver of Vite (node):\n%v\n%s", d.cmd.Process.Pid, kids, d.log)
	}
	return app, driver, kids
}

// However tifa dev ends, the app it started and the driver of Vite end with it and give up their
// ports. Closing a terminal sends SIGHUP, and a script that cleans up ends it with SIGKILL. Under
// the three signals tifa dev stops them itself and exits cleanly, and under SIGKILL it runs nothing.
func TestDevLeavesNothingBehind(t *testing.T) {
	skipUnlessEnabled(t)
	for _, tc := range []struct {
		name     string
		sig      syscall.Signal
		graceful bool
	}{
		{"SIGHUP", syscall.SIGHUP, true},
		{"SIGTERM", syscall.SIGTERM, true},
		{"SIGINT", syscall.SIGINT, true},
		{"SIGKILL", syscall.SIGKILL, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := startDev(t, devApp(t))
			d.waitLog(t, 0, ready)
			expectBody(t, d.base+"/", "North shop")
			app, driver, kids := childrenOf(t, d)
			_, devPort, _ := strings.Cut(strings.TrimPrefix(d.base, "http://"), ":")
			ports := []string{devPort}
			for _, p := range []process{app, driver} {
				found := listenPorts(t, p.pid)
				if len(found) == 0 {
					t.Fatalf("%v listens on no port", p)
				}
				ports = append(ports, found...)
			}

			// d.exited tells nothing while a process that tifa dev left behind holds its output
			// pipe, since Wait waits for the pipe to end, so the test looks at the process.
			dev, ok := processTable(t)[d.cmd.Process.Pid]
			if !ok {
				t.Fatalf("ps does not list tifa dev %d", d.cmd.Process.Pid)
			}
			d.stopped = !tc.graceful
			if err := d.cmd.Process.Signal(tc.sig); err != nil {
				t.Fatal(err)
			}
			if left := stillRunning([]process{dev}, time.Minute); len(left) > 0 {
				t.Fatalf("tifa dev still runs a minute after %s\n%s", tc.name, d.log)
			}
			// Under a signal tifa dev handles it waits for what it started. After SIGKILL the app
			// finishes its requests and the driver closes Vite, which takes a moment.
			within := 5 * time.Second
			if !tc.graceful {
				within = 30 * time.Second
			}
			left := stillRunning(kids, within)
			if len(left) > 0 {
				t.Errorf("%s left these processes running: %v", tc.name, left)
			}
			for _, port := range ports {
				if reaches(port) {
					t.Errorf("port %s still accepts connections after %s", port, tc.name)
				}
			}
			if tc.graceful && len(left) == 0 {
				select {
				case err := <-d.exited:
					if err != nil {
						t.Errorf("tifa dev exited with %v after %s, want a clean stop\n%s", err, tc.name, d.log)
					}
				case <-time.After(10 * time.Second):
					t.Errorf("tifa dev ended after %s but its exit was not reported\n%s", tc.name, d.log)
				}
			}
		})
	}
}

// What tifa dev inherited from the program that started it, such as the lock of a script that
// runs it under flock, stays with tifa dev. The app and the driver of Vite would keep the lock
// after tifa dev is gone.
func TestDevKeepsInheritedFilesOutOfItsChildren(t *testing.T) {
	skipUnlessEnabled(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "lock")
	lock, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	// Descriptor 6: tifa dev starts the app with none, and the driver with 3 for its answers.
	d := startDevWith(t, devApp(t), []*os.File{nil, nil, nil, lock})
	d.waitLog(t, 0, ready)
	expectBody(t, d.base+"/", "North shop")
	app, driver, _ := childrenOf(t, d)

	if !slices.Contains(lsof(t, d.cmd.Process.Pid), lockPath) {
		t.Fatalf("tifa dev does not hold the lock file %s, the test inherits nothing", lockPath)
	}
	for _, p := range []process{app, driver} {
		if slices.Contains(lsof(t, p.pid), lockPath) {
			t.Errorf("%v holds the lock file that tifa dev inherited", p)
		}
	}
}
