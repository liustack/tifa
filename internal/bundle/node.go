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
	"sync"

	"github.com/liustack/tifa/js"
)

// Node runs the driver (js/vite/driver.mjs) on Node in an application root, where it loads the
// application's own Vite. The process starts with the first request and serves every later one,
// so Vite loads once. A Node is used by one goroutine at a time.
type Node struct {
	root   string
	output io.Writer // gets what Node prints, Vite's warnings and errors among it

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	pipe    *os.File // the read end of file descriptor 3
	answers *bufio.Scanner
	next    int
	err     error // why the process is gone, nil while it runs or before it starts
}

// NewNode prepares a driver for the application at root. Nothing runs until the first request.
// output receives what Node and Vite print.
func NewNode(root string, output io.Writer) *Node {
	return &Node{root: root, output: output}
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
	cmd := exec.Command(node, "--input-type=module", "-e", js.ViteDriver)
	cmd.Dir = n.root
	cmd.Stdout, cmd.Stderr = n.output, n.output
	cmd.ExtraFiles = []*os.File{w} // file descriptor 3
	stdin, err := cmd.StdinPipe()
	if err != nil {
		r.Close()
		w.Close()
		return err
	}
	err = cmd.Start()
	// Node has its own copy of the write end now. Closing this one lets reads see Node exit.
	w.Close()
	if err != nil {
		r.Close()
		return fmt.Errorf("starting node: %w", err)
	}
	n.cmd, n.stdin, n.pipe = cmd, stdin, r
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
			err = errors.New("the Vite driver exited")
			if waitErr := n.cmd.Wait(); waitErr != nil {
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
	if n.cmd == nil {
		if err := n.start(); err != nil {
			n.err = err
			n.stop()
			return err
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
		n.err = fmt.Errorf("the Vite driver stopped: %w", err)
		n.stop()
		return n.err
	}
	var raw json.RawMessage
	if err := n.read(&raw); err != nil {
		n.err = err
		n.stop()
		return err
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

// stop ends a process that failed. Call it with n.mu held.
func (n *Node) stop() {
	if n.cmd == nil || n.cmd.Process == nil {
		return
	}
	n.stdin.Close()
	_ = n.cmd.Process.Kill()
	_ = n.cmd.Wait()
	n.pipe.Close()
}

// Close ends the driver: it closes stdin, which stops Vite's dev server, and waits for Node to exit.
func (n *Node) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cmd == nil || n.err != nil {
		return nil
	}
	n.err = errors.New("the Vite driver is closed")
	n.stdin.Close()
	for n.answers.Scan() {
	}
	defer n.pipe.Close()
	return n.cmd.Wait()
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
