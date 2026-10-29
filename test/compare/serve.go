package compare

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Target is a server to measure. Args hold %PORT% where the port goes.
type Target struct {
	Name string
	Dir  string
	Prog string
	Args []string
	Env  []string
}

// Targets are the four servers, in the order they are measured: tifa, Next.js, Next.js without
// its JIT and TanStack Start. tifa runs one VM on one OS thread and renders every request
// (no HTML cache), the Node servers run in production mode.
func (e Env) Targets() []Target {
	nextBin := filepath.Join("node_modules", "next", "dist", "bin", "next")
	return []Target{
		{
			Name: "tifa", Dir: e.Shop, Prog: filepath.Join(e.Shop, "bin", "shop"),
			Args: []string{"--server-addr", "127.0.0.1:%PORT%", "--ssr-vms", "1", "--cache-size", "0"},
			Env:  []string{"GOMAXPROCS=1"},
		},
		{
			Name: "nextjs", Dir: e.Next, Prog: "node",
			Args: []string{nextBin, "start", "-H", "127.0.0.1", "-p", "%PORT%"},
			Env:  []string{"NODE_ENV=production", "NEXT_TELEMETRY_DISABLED=1"},
		},
		{
			Name: "nextjs-jitless", Dir: e.Next, Prog: "node",
			Args: []string{"--jitless", nextBin, "start", "-H", "127.0.0.1", "-p", "%PORT%"},
			Env:  []string{"NODE_ENV=production", "NEXT_TELEMETRY_DISABLED=1"},
		},
		{
			Name: "start", Dir: e.Start, Prog: "node",
			Args: []string{filepath.Join(".output", "server", "index.mjs")},
			Env:  []string{"NODE_ENV=production", "HOST=127.0.0.1", "PORT=%PORT%"},
		},
	}
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

// Start starts the server on a free port, pinned to cpus (a list such as "0" or "0,1" for
// taskset, on Linux only, empty for no pinning), and returns once it answers the first page.
func (t Target) Start(ctx context.Context, cpus string) (*Server, error) {
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
	// A tifa program takes every TIFA_* variable for a setting and rejects the unknown ones,
	// TIFA_COMPARE among them.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TIFA_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, t.Env...)
	for i := range cmd.Env {
		cmd.Env[i] = strings.ReplaceAll(cmd.Env[i], "%PORT%", strconv.Itoa(port))
	}
	s := &Server{Name: t.Name, Base: "http://127.0.0.1:" + strconv.Itoa(port), cmd: cmd, out: &lockedBuffer{}, stop: make(chan struct{}), done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = s.out, s.out
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", t.Name, err)
	}
	exited := make(chan struct{})
	s.exited = exited
	go func() { cmd.Wait(); close(exited) }()
	go s.watch()
	deadline := time.Now().Add(60 * time.Second)
	for {
		res, err := http.Get(s.Base + Pages[0].URL)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		select {
		case <-exited:
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
	return s, nil
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
