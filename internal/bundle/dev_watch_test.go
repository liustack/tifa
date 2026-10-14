package bundle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// viteModule returns the module Vite's dev server serves for path, relative to the root.
func viteModule(t *testing.T, port int, path string) string {
	t.Helper()
	res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s%s", port, DevBase, path))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("%s: %d %s", path, res.StatusCode, body)
	}
	return string(body)
}

// Vite's dev server watches the application root itself, so a plugin or Vite sees the files as
// its own watcher reports them: by their real path, with every event. A module reached through a
// linked folder is known by its real path, and an edit there reaches the browser.
func TestDevServerWatchesTheRoot(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{
		"app/real/card.tsx": "export const card = 'card-one'\n",
		"app/page/view.tsx": "import { card } from './link/card'\n" +
			"export default function View() { return <p>{card}</p> }\n",
	})
	if err := os.Symlink(filepath.Join(root, "app/real"), filepath.Join(root, "app/page/link")); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if code := viteModule(t, port, "app/real/card.tsx"); !strings.Contains(code, "card-one") {
		t.Fatalf("the module is not served by its real path: %s", code)
	}
	writeTree(t, root, map[string]string{"app/real/card.tsx": "export const card = 'card-two'\n"})
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(viteModule(t, port, "app/real/card.tsx"), "card-two") {
		if time.Now().After(deadline) {
			t.Fatal("Vite's dev server serves the old module 5s after the edit")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// viteConfig is the Vite config of devApp with server added to it.
func viteConfig(server string) string {
	return "import react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
		"export default { plugins: [tifa(), react()], server: " + server + " }\n"
}

// When tifa dev polls because the system ran out of watches, Vite's dev server, told by
// PollFiles, polls too: it restarts with a watcher that polls, which takes no watch or descriptor
// per file, and edits still reach it.
func TestDevServerPollsWhenTold(t *testing.T) {
	root := devApp(t)
	// FSEvents takes no descriptor per file, so Vite's watcher would have nothing to give back.
	writeTree(t, root, map[string]string{"vite.config.ts": viteConfig("{ watch: { useFsEvents: false, usePolling: false } }")})
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	polling, err := d.PollFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !polling {
		t.Fatal("Vite's dev server does not poll")
	}
	viteModule(t, port, "app/page/view.tsx")
	writeTree(t, root, map[string]string{"app/page/view.tsx": "export default function View() { return <p>polled</p> }\n"})
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(viteModule(t, port, "app/page/view.tsx"), "polled") {
		if time.Now().After(deadline) {
			t.Fatal("the polling dev server serves the old module 5s after the edit")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Told before it runs, the dev server starts polling, unless FSEvents watches for it: that takes
// no descriptor per file.
func TestDevServerStartsPolling(t *testing.T) {
	root := devApp(t)
	d, err := NewDev(root, freePort(t), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if polling, err := d.PollFiles(); err != nil || polling {
		t.Fatalf("PollFiles before the dev server runs = %v, %v", polling, err)
	}
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	var watcher struct{ Polling, FSEvents bool }
	if err := d.node.call("pollFiles", nil, &watcher); err != nil {
		t.Fatal(err)
	}
	if watcher.Polling == watcher.FSEvents {
		t.Fatalf("watcher %+v, want it to poll unless it uses FSEvents", watcher)
	}
}

// When Vite's own watcher runs out of watches or file descriptors, which chokidar reports as an
// error event, the dev server restarts with a watcher that polls.
func TestDevServerPollsWhenItRunsOut(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{"vite.config.ts": viteConfig("{ middlewareMode: true, watch: { useFsEvents: false, usePolling: false } }")})
	script := `
import { createServer } from 'vite'
const server = await createServer({ root: process.cwd(), logLevel: 'silent' })
const before = server.watcher
const err = Object.assign(new Error('ENOSPC: System limit for number of file watchers reached'), { code: 'ENOSPC' })
server.watcher.emit('error', err)
const deadline = Date.now() + 10000
while (server.watcher === before && Date.now() < deadline) await new Promise((resolve) => setTimeout(resolve, 50))
console.log(JSON.stringify({ restarted: server.watcher !== before, polling: server.watcher.options.usePolling === true }))
await server.close()
process.exit(0)
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", script)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != `{"restarted":true,"polling":true}` {
		t.Fatalf("got %s, want the dev server restarted with a polling watcher", got)
	}
}
