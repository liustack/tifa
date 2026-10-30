package bundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/testproc"
)

// devParentRoot names, in the environment of this test binary, the application a copy of it
// runs tifa dev for, as the parent of a Vite driver that a test then kills.
const devParentRoot = "TIFA_TEST_DEV_PARENT_ROOT"

func TestMain(m *testing.M) {
	if root := os.Getenv(devParentRoot); root != "" {
		devParent(root)
		return
	}
	os.Exit(m.Run())
}

// devParent builds the dev output of root with a short close timeout, and waits to be killed.
func devParent(root string) {
	closeTimeout = 500 * time.Millisecond
	port, err := strconv.Atoi(os.Getenv("TIFA_TEST_DEV_PARENT_PORT"))
	if err != nil {
		panic(err)
	}
	d, err := NewDev(root, port, io.Discard, nil)
	if err != nil {
		panic(err)
	}
	_, err = d.Build(context.Background(), filepath.Join(root, "public"), true)
	fmt.Fprintln(os.Stderr, "the build ended:", err)
	select {}
}

// alive reports whether the process pid exists.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// stuckApp returns an application whose SSR build never ends. Its SSR worker writes the pid of
// the driver to the file marker when the build starts.
func stuckApp(t *testing.T) (root, marker string) {
	root = devApp(t)
	marker = filepath.Join(root, "driver.pid")
	writeTree(t, root, map[string]string{"vite.config.ts": "import react from '@vitejs/plugin-react'\n" +
		"import tifa from './.tifa/vite.js'\nimport fs from 'node:fs'\nimport { isMainThread } from 'node:worker_threads'\n" +
		"const stuck = { name: 'stuck', buildStart() {\n" +
		"  if (isMainThread) return\n" +
		"  fs.writeFileSync(" + strconv.Quote(marker) + ", String(process.pid))\n" +
		"  return new Promise(() => {})\n" +
		"} }\n" +
		"export default { plugins: [tifa(), react(), stuck] }\n"})
	return root, marker
}

// driverPid waits for the pid of the driver in the file marker. The driver is killed at the end
// of the test, so that no test leaves one behind, whatever it finds.
func driverPid(t *testing.T, marker string) int {
	t.Helper()
	for start := time.Now(); time.Since(start) < 30*time.Second; time.Sleep(20 * time.Millisecond) {
		if text, err := os.ReadFile(marker); err == nil && len(text) > 0 {
			pid, err := strconv.Atoi(strings.TrimSpace(string(text)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
			return pid
		}
	}
	t.Fatal("the driver did not write its pid")
	return 0
}

// shortCloseTimeout makes the drivers the test starts close in half a second.
func shortCloseTimeout(t *testing.T) {
	saved := closeTimeout
	closeTimeout = 500 * time.Millisecond
	t.Cleanup(func() { closeTimeout = saved })
}

// gone waits until the process pid has exited.
func gone(t *testing.T, pid int) {
	t.Helper()
	for start := time.Now(); alive(pid); time.Sleep(20 * time.Millisecond) {
		if time.Since(start) > 3*time.Second {
			t.Fatalf("the driver %d still runs", pid)
		}
	}
}

// When tifa dev dies while a build of the SSR bundle never ends, the Vite driver it started
// must not live on: it holds the port of Vite's dev server and watches the files.
func TestDevDriverEndsWhenItsParentDiesDuringABuild(t *testing.T) {
	root, marker := stuckApp(t)
	port := freePort(t)
	parent := exec.Command(os.Args[0])
	parent.Env = append(os.Environ(), devParentRoot+"="+root, "TIFA_TEST_DEV_PARENT_PORT="+strconv.Itoa(port))
	if err := testproc.Start(parent); err != nil {
		t.Fatal(err)
	}
	defer parent.Wait()
	defer parent.Process.Kill()
	pid := driverPid(t, marker)
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	gone(t, pid)
	if conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
		conn.Close()
		t.Errorf("port %d of Vite's dev server is still open", port)
	}
}

// Close returns while a build never ends, which ends the build with an error, and the driver is
// gone when it returns: tifa dev stopped with Ctrl-C does not wait for the build.
func TestDevCloseEndsABuildThatNeverEnds(t *testing.T) {
	shortCloseTimeout(t)
	root, marker := stuckApp(t)
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	built := make(chan error, 1)
	go func() {
		_, err := d.Build(context.Background(), filepath.Join(root, "public"), true)
		built <- err
	}()
	pid := driverPid(t, marker)
	closed := make(chan error, 1)
	go func() { closed <- d.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(closeTimeout + 3*time.Second):
		t.Fatal("Close waits for the build")
	}
	if alive(pid) {
		t.Errorf("the driver %d still runs after Close", pid)
	}
	select {
	case err := <-built:
		if err == nil || !strings.Contains(err.Error(), "the Vite driver was closed before it answered") {
			t.Errorf("Build: %v, want the error of a closed driver", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Build did not return after Close")
	}
}

// A driver that does not exit after its close timeout, here because a plugin blocks its thread
// when stdin ends, is killed.
func TestDevCloseKillsADriverThatDoesNotExit(t *testing.T) {
	shortCloseTimeout(t)
	root := devApp(t)
	marker := filepath.Join(root, "driver.pid")
	writeTree(t, root, map[string]string{"vite.config.ts": "import react from '@vitejs/plugin-react'\n" +
		"import tifa from './.tifa/vite.js'\nimport fs from 'node:fs'\n" +
		"const block = { name: 'block', configureServer() {\n" +
		"  fs.writeFileSync(" + strconv.Quote(marker) + ", String(process.pid))\n" +
		"  process.stdin.on('end', () => Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0))\n" +
		"} }\n" +
		"export default { plugins: [tifa(), react(), block] }\n"})
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatal(err)
	}
	pid := driverPid(t, marker)
	closed := make(chan error, 1)
	go func() { closed <- d.Close() }()
	select {
	case err := <-closed:
		if err == nil || !strings.Contains(err.Error(), "killed") {
			t.Errorf("Close: %v, want the error of a killed driver", err)
		}
	case <-time.After(closeTimeout + 3*time.Second):
		t.Fatal("Close waits for a driver that does not exit")
	}
	if alive(pid) {
		t.Errorf("the driver %d still runs after Close", pid)
	}
}

// The driver takes over the stdin listener with which Vite's dev server ends the process, and
// leaves those of the plugins: they still learn that stdin ended.
func TestDevKeepsTheStdinListenersOfPlugins(t *testing.T) {
	root := devApp(t)
	marker := filepath.Join(root, "stdin-ended")
	writeTree(t, root, map[string]string{"vite.config.ts": "import react from '@vitejs/plugin-react'\n" +
		"import tifa from './.tifa/vite.js'\nimport fs from 'node:fs'\n" +
		"const listen = { name: 'listen', configureServer() {\n" +
		"  process.stdin.on('end', () => fs.writeFileSync(" + strconv.Quote(marker) + ", 'ended'))\n" +
		"} }\n" +
		"export default { plugins: [tifa(), react(), listen] }\n"})
	devBuild(t, root)
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the stdin listener of a plugin did not run: %v", err)
	}
}

// tifa dev stops by ending the context of the build it runs, on Ctrl-C or SIGTERM. A build that
// never ends then ends with the driver, so that tifa dev exits.
func TestDevBuildEndsWithItsContext(t *testing.T) {
	shortCloseTimeout(t)
	root, marker := stuckApp(t)
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	built := make(chan error, 1)
	go func() {
		_, err := d.Build(ctx, filepath.Join(root, "public"), true)
		built <- err
	}()
	pid := driverPid(t, marker)
	cancel()
	select {
	case err := <-built:
		if !errors.Is(err, errInterrupted) {
			t.Errorf("Build: %v, want %v", err, errInterrupted)
		}
	case <-time.After(closeTimeout + 3*time.Second):
		t.Fatal("Build waits after its context ended")
	}
	gone(t, pid)
}
