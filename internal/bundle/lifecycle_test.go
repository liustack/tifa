package bundle

import (
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
	d, err := NewDev(root, port, io.Discard)
	if err != nil {
		panic(err)
	}
	_, err = d.Build(filepath.Join(root, "public"), true)
	fmt.Fprintln(os.Stderr, "the build ended:", err)
	select {}
}

// alive reports whether the process pid exists.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// When tifa dev dies while a build of the SSR bundle never ends, the Vite driver it started
// must not live on: it holds the port of Vite's dev server and watches the files.
func TestDevDriverEndsWhenItsParentDiesDuringABuild(t *testing.T) {
	root := devApp(t)
	marker := filepath.Join(root, "driver.pid")
	writeTree(t, root, map[string]string{"vite.config.ts": "import react from '@vitejs/plugin-react'\n" +
		"import tifa from './.tifa/vite.js'\nimport fs from 'node:fs'\nimport { isMainThread } from 'node:worker_threads'\n" +
		"const stuck = { name: 'stuck', buildStart() {\n" +
		"  if (isMainThread) return\n" +
		"  fs.writeFileSync(" + strconv.Quote(marker) + ", String(process.pid))\n" +
		"  return new Promise(() => {})\n" +
		"} }\n" +
		"export default { plugins: [tifa(), react(), stuck] }\n"})
	port := freePort(t)
	parent := exec.Command(os.Args[0])
	parent.Env = append(os.Environ(), devParentRoot+"="+root, "TIFA_TEST_DEV_PARENT_PORT="+strconv.Itoa(port))
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	defer parent.Wait()
	defer parent.Process.Kill()
	var pid int
	for start := time.Now(); pid == 0; time.Sleep(20 * time.Millisecond) {
		if time.Since(start) > 30*time.Second {
			t.Fatal("the SSR build did not start")
		}
		if text, err := os.ReadFile(marker); err == nil && len(text) > 0 {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(text)))
		}
	}
	// Leave no driver behind, whatever the test finds.
	defer syscall.Kill(pid, syscall.SIGKILL)
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		t.Fatalf("the driver %d lives on after its parent died", pid)
	}
	if conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
		conn.Close()
		t.Errorf("port %d of Vite's dev server is still open", port)
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
