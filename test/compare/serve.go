package compare

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liustack/tifa/internal/testproc"
)

// Target is a server to measure. Args hold %PORT% where the port goes.
type Target struct {
	Name string
	Dir  string
	Prog string
	Args []string
	Env  []string
}

// Server is a running Target.
type Server struct {
	Name string
	Base string // http://127.0.0.1:<port>

	cmd    *exec.Cmd
	out    *lockedBuffer
	mu     sync.Mutex
	peak   int64 // the largest resident size seen, in bytes, of the process and its children
	stop   chan struct{}
	done   chan struct{}
	exited chan struct{}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Launch starts the server on a free port, pinned to cpus (a list such as "0" or "0,1" for
// taskset, on Linux only, empty for no pinning), and returns at once, before it listens.
func (t Target) Launch(cpus string) (*Server, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	args := make([]string, len(t.Args))
	for i, a := range t.Args {
		args[i] = strings.ReplaceAll(a, "%PORT%", strconv.Itoa(port))
	}
	prog := t.Prog
	if cpus != "" {
		if runtime.GOOS != "linux" {
			return nil, fmt.Errorf("pinning to cpus needs taskset, which is on Linux only: leave the cpus empty on %s", runtime.GOOS)
		}
		args = append([]string{"-c", cpus, prog}, args...)
		prog = "taskset"
	}
	cmd := exec.Command(prog, args...)
	cmd.Dir = t.Dir
	cmd.Env = append(os.Environ(), t.Env...)
	for i := range cmd.Env {
		cmd.Env[i] = strings.ReplaceAll(cmd.Env[i], "%PORT%", strconv.Itoa(port))
	}
	s := &Server{Name: t.Name, Base: "http://127.0.0.1:" + strconv.Itoa(port), cmd: cmd, out: &lockedBuffer{}, stop: make(chan struct{}), done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = s.out, s.out
	if err := testproc.Start(cmd); err != nil {
		return nil, fmt.Errorf("start %s: %w", t.Name, err)
	}
	exited := make(chan struct{})
	s.exited = exited
	go func() { cmd.Wait(); close(exited) }()
	go s.watch()
	return s, nil
}

// Start is Launch that returns once the server answers the first page.
func (t Target) Start(ctx context.Context, cpus string) (*Server, error) {
	s, err := t.Launch(cpus)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		res, err := http.Get(s.Base + Pages[0].URL)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return s, nil
			}
		}
		select {
		case <-s.exited:
			return nil, fmt.Errorf("%s exited before it answered:\n%s", t.Name, s.out)
		case <-ctx.Done():
			s.Stop()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			s.Stop()
			return nil, fmt.Errorf("%s did not answer in 60 seconds:\n%s", t.Name, s.out)
		}
	}
}

// WaitListening returns once the server accepts connections, polling every 2 ms, without
// sending a request. A server that exits first or does not listen in 60 seconds is an error.
func (s *Server) WaitListening(ctx context.Context) error {
	addr := strings.TrimPrefix(s.Base, "http://")
	deadline := time.Now().Add(60 * time.Second)
	for {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return nil
		}
		select {
		case <-s.exited:
			return fmt.Errorf("%s exited before it listened:\n%s", s.Name, s.out)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not listen in 60 seconds:\n%s", s.Name, s.out)
		}
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// watch samples the resident size of the process and its children until Stop.
func (s *Server) watch() {
	defer close(s.done)
	for {
		cur, _ := s.rss()
		s.mu.Lock()
		s.peak = max(s.peak, cur)
		s.mu.Unlock()
		select {
		case <-s.stop:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// PeakRSS is the largest resident size of the server and its children in bytes. On Linux it is
// the larger of the sampled sizes and the kernel's high-water marks, summed over the processes.
func (s *Server) PeakRSS() int64 {
	_, hwm := s.rss()
	s.mu.Lock()
	defer s.mu.Unlock()
	return max(s.peak, hwm)
}

// rss returns the resident size now and, on Linux, the sum of the high-water marks.
func (s *Server) rss() (cur, hwm int64) {
	pids := tree(s.cmd.Process.Pid)
	if runtime.GOOS == "linux" {
		for _, pid := range pids {
			data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				f := strings.Fields(line)
				if len(f) < 2 {
					continue
				}
				kb, _ := strconv.ParseInt(f[1], 10, 64)
				switch f[0] {
				case "VmRSS:":
					cur += kb << 10
				case "VmHWM:":
					hwm += kb << 10
				}
			}
		}
		return cur, hwm
	}
	for _, pid := range pids {
		out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			continue
		}
		kb, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		cur += kb << 10
	}
	return cur, 0
}

// tree lists a process and all its descendants.
func tree(root int) []int {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return []int{root}
	}
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		children[ppid] = append(children[ppid], pid)
	}
	list := []int{root}
	for i := 0; i < len(list); i++ {
		list = append(list, children[list[i]]...)
	}
	return list
}

// Stop kills the server and its children. It only touches processes it started.
func (s *Server) Stop() {
	select {
	case <-s.stop:
		return
	default:
	}
	pids := tree(s.cmd.Process.Pid)
	close(s.stop)
	for i := len(pids) - 1; i >= 0; i-- {
		if p, err := os.FindProcess(pids[i]); err == nil {
			p.Kill()
		}
	}
	<-s.exited
	<-s.done
}

// Output is what the server has printed.
func (s *Server) Output() string { return s.out.String() }

// RSS is the resident size of the server and its children now, in bytes.
func (s *Server) RSS() int64 {
	cur, _ := s.rss()
	return cur
}

// CPUSeconds is the processor time, user and system, that the server and its children have used so
// far, in seconds. It reads /proc, so it is 0 off Linux. It includes children that have exited and
// that the server has waited for.
func (s *Server) CPUSeconds() float64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	var ticks float64
	for _, pid := range tree(s.cmd.Process.Pid) {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			continue
		}
		// The name is in parentheses and may hold spaces: the fields we want come after the last ")".
		rest := string(data[strings.LastIndexByte(string(data), ')')+1:])
		f := strings.Fields(rest)
		if len(f) < 15 {
			continue
		}
		for _, i := range []int{11, 12, 13, 14} { // utime stime cutime cstime
			n, _ := strconv.ParseFloat(f[i], 64)
			ticks += n
		}
	}
	return ticks / 100 // CLK_TCK is 100 on Linux
}
