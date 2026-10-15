package bundle

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liustack/tifa/js"
)

// Node runs the driver (js/vite/driver.mjs) on Node in an application root, where it loads the
// application's own Vite. The process starts with the first request and serves every later one,
// so Vite loads once. Requests run one at a time, and Close may run during one.
type Node struct {
	root   string
	output io.Writer // gets what Node prints, Vite's warnings and errors among it
	mode   string    // NODE_ENV of the process: production, or development for tifa dev

	mu      sync.Mutex // held by a request
	answers *bufio.Scanner
	next    int
	err     error // why the process is gone, nil while it runs or before it starts

	process sync.Mutex // guards the fields below, which start sets and Close reads
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	pipe    *os.File      // the read end of file descriptor 3
	exited  chan struct{} // closed once the process has exited, waitErr then tells how
	waitErr error
	closing bool // Close was called: no request starts the process again

	closeOnce sync.Once
	closeErr  error
}

// closeTimeout is how long the driver may take to close once its stdin ends, when tifa closes it
// or dies. It waits for the request it runs and its output, and after this time stops and exits,
// so a build that never ends does not keep it, and Vite's dev server, alive. Close kills a driver
// that is still there a second later.
var closeTimeout = 5 * time.Second

var (
	errClosed      = errors.New("the Vite driver is closed")                     // of a request after Close
	errInterrupted = errors.New("the Vite driver was closed before it answered") // of a request Close interrupts
)

// NewNode prepares a driver for the application at root. Nothing runs until the first request.
// output receives what Node and Vite print. Its builds are production builds, whatever NODE_ENV
// tifa runs with.
func NewNode(root string, output io.Writer) *Node {
	return &Node{root: root, output: output, mode: "production"}
}

// answer is one line the driver writes: the id of the request, an error, and the result fields.
type answer struct {
	ID    int    `json:"id"`
	Error string `json:"error"`
}

// start runs the driver and reads its first answer, which tells whether Vite loaded.
func (n *Node) start() error {
	node, err := exec.LookPath("node")
	if err != nil {
		return errors.New("tifa bundles the front end with Vite, which runs on Node, and node is not on PATH")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := exec.Command(node, "--input-type=module", "-e", js.ViteDriver, strconv.FormatInt(closeTimeout.Milliseconds(), 10))
	cmd.Dir = n.root
	// Vite and @vitejs/plugin-react pick the React build and the JSX transform by NODE_ENV, so
	// tifa sets it rather than take it from the shell.
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "NODE_ENV=")
	}), "NODE_ENV="+n.mode)
	cmd.Stdout, cmd.Stderr = n.output, n.output
	cmd.ExtraFiles = []*os.File{w} // file descriptor 3
	// A process Node starts may keep stdout and stderr open after Node exits.
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		r.Close()
		w.Close()
		return err
	}
	n.process.Lock()
	if n.closing {
		n.process.Unlock()
		r.Close()
		w.Close()
		return errClosed
	}
	err = cmd.Start()
	// Node has its own copy of the write end now. Closing this one lets reads see Node exit.
	w.Close()
	if err != nil {
		n.process.Unlock()
		r.Close()
		return fmt.Errorf("starting node: %w", err)
	}
	exited := make(chan struct{})
	n.cmd, n.stdin, n.pipe, n.exited = cmd, stdin, r, exited
	n.process.Unlock()
	go func() {
		err := cmd.Wait()
		n.process.Lock()
		n.waitErr = err
		n.process.Unlock()
		close(exited)
	}()
	n.answers = bufio.NewScanner(r)
	n.answers.Buffer(nil, 64<<20)
	var first answer
	if err := n.read(&first); err != nil {
		return err
	}
	if first.Error != "" {
		return errors.New(first.Error)
	}
	return nil
}

// read decodes the next answer into v.
func (n *Node) read(v any) error {
	if !n.answers.Scan() {
		err := n.answers.Err()
		if err == nil {
			<-n.exited
			err = errors.New("the Vite driver exited")
			if waitErr := n.exitErr(); waitErr != nil {
				err = fmt.Errorf("the Vite driver exited: %w", waitErr)
			}
		}
		return err
	}
	return json.Unmarshal(n.answers.Bytes(), v)
}

// call sends the request op with the fields of req and decodes the answer into result, which may
// be nil. A failure of Vite, such as a build error, comes back as the error.
func (n *Node) call(op string, req map[string]any, result any) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	n.process.Lock()
	closing := n.closing
	n.process.Unlock()
	if closing {
		n.err = errClosed
		return n.err
	}
	if n.cmd == nil {
		if err := n.start(); err != nil {
			return n.fail(err)
		}
	}
	n.next++
	msg := map[string]any{"id": n.next, "op": op}
	for k, v := range req {
		msg[k] = v
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := n.stdin.Write(append(line, '\n')); err != nil {
		return n.fail(fmt.Errorf("the Vite driver stopped: %w", err))
	}
	var raw json.RawMessage
	if err := n.read(&raw); err != nil {
		return n.fail(err)
	}
	var a answer
	if err := json.Unmarshal(raw, &a); err != nil {
		return err
	}
	if a.ID != n.next {
		return fmt.Errorf("the Vite driver answered request %d, want %d", a.ID, n.next)
	}
	if a.Error != "" {
		return errors.New(a.Error)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(raw, result)
}

// fail ends the process after err, and returns the error of every later request. When Close
// caused err, the process is left to Close, which lets it end on its own first. Call it with n.mu
// held.
func (n *Node) fail(err error) error {
	n.process.Lock()
	closing := n.closing
	n.process.Unlock()
	if closing {
		n.err = errInterrupted
		return n.err
	}
	n.err = err
	n.stop()
	return err
}

// exitErr is how the process exited, once it has.
func (n *Node) exitErr() error {
	n.process.Lock()
	defer n.process.Unlock()
	return n.waitErr
}

// stop ends a process that failed, and leaves nothing for Close to do. Call it with n.mu held.
func (n *Node) stop() {
	n.process.Lock()
	n.closing = true
	n.process.Unlock()
	if n.cmd == nil {
		return
	}
	n.stdin.Close()
	_ = n.cmd.Process.Kill()
	<-n.exited
	n.pipe.Close()
}

// Close ends the driver: it closes stdin, which stops Vite's dev server, and waits for Node to
// exit, closeTimeout and a second at most, then kills it. A request it interrupts returns
// errInterrupted. Close may run during a request, and later calls return what the first returned.
func (n *Node) Close() error {
	n.closeOnce.Do(func() { n.closeErr = n.close() })
	return n.closeErr
}

func (n *Node) close() error {
	n.process.Lock()
	stopped := n.closing
	n.closing = true
	cmd, stdin, pipe, exited := n.cmd, n.stdin, n.pipe, n.exited
	n.process.Unlock()
	if cmd == nil || stopped {
		return nil
	}
	// Closing the read end of file descriptor 3 ends a request that still reads it, should a
	// process Node started keep the write end open.
	defer pipe.Close()
	stdin.Close()
	wait := closeTimeout + time.Second
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-exited:
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-exited
		return fmt.Errorf("the Vite driver did not exit %s after its stdin closed and was killed", wait)
	}
	return n.exitErr()
}

// Exports returns the names each file exports, sorted, by file. The files are TypeScript or
// JavaScript, with or without JSX, and are parsed alone: nothing runs and imports stay unresolved.
// Types are left out, and so are the names re-exported with export * from another module.
func (n *Node) Exports(files []string) (map[string][]string, error) {
	abs := make([]string, len(files))
	for i, f := range files {
		var err error
		if abs[i], err = filepath.Abs(f); err != nil {
			return nil, err
		}
	}
	var result struct{ Exports map[string][]string }
	if err := n.call("exports", map[string]any{"files": abs}, &result); err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(files))
	for i, f := range files {
		out[f] = result.Exports[abs[i]]
	}
	return out, nil
}
