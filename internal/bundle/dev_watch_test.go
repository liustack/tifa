package bundle

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	d, err := NewDev(root, port, io.Discard, nil)
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

// Vite's dev server watches the files outside the root that it loads, such as a package of the
// workspace, and tifa dev does not. Dev reports their changes, so the SSR bundle is built again.
func TestDevReportsChangesOutsideTheRoot(t *testing.T) {
	root := devApp(t)
	shared := filepath.Join(filepath.Dir(root), "shared")
	writeTree(t, shared, map[string]string{"card.ts": "export const card = 'outside-one'\n"})
	writeTree(t, root, map[string]string{
		"vite.config.ts": viteConfig("{ fs: { allow: ['..'] } }"),
		"app/page/view.tsx": "import { card } from '../../../shared/card'\n" +
			"export default function View() { return <p>{card}</p> }\n",
	})
	heard := newHeard()
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard, heard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	card, err := filepath.EvalSymlinks(filepath.Join(shared, "card.ts"))
	if err != nil {
		t.Fatal(err)
	}
	// The browser loads the module, which makes Vite watch it.
	if code := viteModule(t, port, "@fs"+filepath.ToSlash(card)); !strings.Contains(code, "outside-one") {
		t.Fatalf("the module outside the root is not served: %s", code)
	}
	writeTree(t, root, map[string]string{"app/page/view.tsx": "export default function View() { return <p>edited</p> }\n"})
	// Vite's watcher adds the file without waiting for the system to watch it, and on a busy
	// machine an edit right after the request can come first. So edit until one is reported.
	deadline := time.After(5 * time.Second)
	for i := 2; ; i++ {
		writeTree(t, shared, map[string]string{"card.ts": fmt.Sprintf("export const card = 'outside-%d'\n", i)})
		select {
		case got := <-heard.changed:
			if got != card {
				t.Fatalf("Dev reported %s, want %s, files in the root are tifa dev's", got, card)
			}
			return
		case <-time.After(250 * time.Millisecond):
		case <-deadline:
			t.Fatal("no change reported within 5s")
		}
	}
}

// heard collects what Dev reports, see Notices.
type heard struct {
	changed   chan string
	restarted chan struct{}
}

func newHeard() *heard { return &heard{make(chan string, 100), make(chan struct{}, 100)} }

func (h *heard) Changed(file string) { h.changed <- file }
func (h *heard) Restarted()          { h.restarted <- struct{}{} }

// Once Vite's dev server restarted, Dev says so, as a file outside the root may have changed
// meanwhile with no event. The new watcher goes on watching the files outside the root that the
// old one watched, before a browser loads them again.
func TestDevReportsRestartsAndKeepsWatchingOutside(t *testing.T) {
	root := devApp(t)
	shared := filepath.Join(filepath.Dir(root), "shared")
	writeTree(t, shared, map[string]string{"card.ts": "export const card = 'outside-one'\n"})
	config := func(note string) string {
		return "import react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
			"export default { plugins: [tifa(), react()], server: { fs: { allow: ['..'] }, watch: { useFsEvents: false } } } // " + note + "\n"
	}
	writeTree(t, root, map[string]string{
		"vite.config.ts": config("one"),
		"app/page/view.tsx": "import { card } from '../../../shared/card'\n" +
			"export default function View() { return <p>{card}</p> }\n",
	})
	heard := newHeard()
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard, heard)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	card, err := filepath.EvalSymlinks(filepath.Join(shared, "card.ts"))
	if err != nil {
		t.Fatal(err)
	}
	viteModule(t, port, "@fs"+filepath.ToSlash(card))
	writeTree(t, root, map[string]string{"vite.config.ts": config("two")})
	select {
	case <-heard.restarted:
	case <-time.After(15 * time.Second):
		t.Fatal("no restart reported within 15s after the config changed")
	}
	deadline := time.After(5 * time.Second)
	for i := 2; ; i++ {
		writeTree(t, shared, map[string]string{"card.ts": fmt.Sprintf("export const card = 'outside-%d'\n", i)})
		select {
		case got := <-heard.changed:
			if got != card {
				t.Fatalf("Dev reported %s, want %s", got, card)
			}
			return
		case <-time.After(250 * time.Millisecond):
		case <-deadline:
			t.Fatal("no change outside the root reported within 5s after the restart")
		}
	}
}

// When tifa dev polls because the system ran out of watches, Vite's dev server, told by
// PollFiles, polls too: it restarts with a watcher that polls, which takes no watch or descriptor
// per file, and edits still reach it.
func TestDevServerPollsWhenTold(t *testing.T) {
	root := devApp(t)
	// FSEvents takes no descriptor per file, so Vite's watcher would have nothing to give back.
	writeTree(t, root, map[string]string{"vite.config.ts": viteConfig("{ watch: { useFsEvents: false, usePolling: false } }")})
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard, nil)
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
	d, err := NewDev(root, freePort(t), io.Discard, nil)
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

// waitFile waits until name exists.
func waitFile(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(name); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear within 10s", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Vite restarts its dev server after a change to its config, and closes the old one. An SSR
// build that runs meanwhile waits for the new dev server to load its assets from, and builds with
// the new config, instead of failing on the dev server that closes.
func TestDevSSRWaitsForARestart(t *testing.T) {
	root := devApp(t)
	closing := filepath.Join(root, "closing")
	config := func(value string) string {
		return "import fs from 'node:fs'\nimport react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
			"const slowClose = { name: 'slow-close', apply: 'serve', async closeBundle() {\n" +
			"  fs.writeFileSync(" + strconv.Quote(closing) + ", '')\n" +
			"  await new Promise((resolve) => setTimeout(resolve, 1500))\n} }\n" +
			"export default { plugins: [tifa(), react(), slowClose], define: { __VALUE__: " + strconv.Quote(strconv.Quote(value)) + " } }\n"
	}
	writeTree(t, root, map[string]string{
		"vite.config.ts": config("one"),
		// Large enough not to be inlined, so the dev server gives its URL.
		"app/page/logo.svg": `<svg xmlns="http://www.w3.org/2000/svg"><!--` + strings.Repeat("x", 8<<10) + `--></svg>`,
		".tifa/server.ts": "import logo from '../app/page/logo.svg?url'\n" +
			"declare const __VALUE__: string\n(globalThis as any).result = __VALUE__ + ' ' + logo\n",
	})
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	result := func() string {
		t.Helper()
		if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
			t.Fatalf("Build: %v", err)
		}
		code, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
		if err != nil {
			t.Fatal(err)
		}
		return evalResult(t, string(code))
	}
	logo := DevBase + "app/page/logo.svg"
	if got := result(); got != "one "+logo {
		t.Fatalf("SSR bundle gives %q", got)
	}
	writeTree(t, root, map[string]string{"vite.config.ts": config("two")})
	waitFile(t, closing) // the new dev server is up, the old one closes
	if got := result(); got != "two "+logo {
		t.Fatalf("SSR bundle gives %q after the restart, want %q", got, "two "+logo)
	}
}

// When Vite's watcher runs out of watches, the dev server restarts with a watcher that polls. A
// restart that fails, here because the config cannot load for a moment, keeps the old server and
// its watcher, and the next error of the watcher tries again.
func TestDevServerTriesPollingAgain(t *testing.T) {
	root := devApp(t)
	broken := filepath.Join(root, "broken")
	control := "import fs from 'node:fs'\n" +
		"const control = { name: 'control', apply: 'serve',\n" +
		"  config() { if (globalThis.served && fs.existsSync(" + strconv.Quote(broken) + ")) throw new Error('the config does not load for a moment') },\n" +
		"  configureServer(server) {\n" +
		"    globalThis.served = true\n" +
		"    server.middlewares.use('" + DevBase + "__test/enospc', (req, res) => {\n" +
		"      server.watcher.emit('error', Object.assign(new Error('ENOSPC: System limit for number of file watchers reached'), { code: 'ENOSPC' }))\n" +
		"      res.end('ok')\n    })\n" +
		"    server.middlewares.use('" + DevBase + "__test/watcher', (req, res) => res.end(String(server.watcher.options.usePolling === true)))\n" +
		"  } }\n"
	writeTree(t, root, map[string]string{"vite.config.ts": control +
		"import react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
		"export default { plugins: [tifa(), react(), control], server: { watch: { useFsEvents: false, usePolling: false } } }\n"})
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	polls := func() string { return strings.TrimSpace(viteModule(t, port, "__test/watcher")) }
	writeTree(t, root, map[string]string{"broken": ""})
	viteModule(t, port, "__test/enospc")
	time.Sleep(time.Second)
	if got := polls(); got != "false" {
		t.Fatalf("the watcher polls %s, though the restart failed", got)
	}
	if err := os.Remove(broken); err != nil {
		t.Fatal(err)
	}
	viteModule(t, port, "__test/enospc")
	// While the dev server restarts, it closed the old server and the new one does not listen
	// yet, so a request may find nobody.
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s__test/watcher", port, DevBase))
		if err == nil {
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if strings.TrimSpace(string(body)) == "true" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the watcher does not poll 10s after the second error: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A restart can also fail once the old server closed, when the new one cannot listen because
// another program took the port. The driver goes on, and the next restart, here after a change to
// the config, brings the dev server back.
func TestDevServerSurvivesARestartThatCannotListen(t *testing.T) {
	root := devApp(t)
	closing := filepath.Join(root, "closing")
	config := func(note string) string {
		return "import fs from 'node:fs'\n" +
			"const control = { name: 'control', apply: 'serve',\n" +
			"  configureServer(server) {\n" +
			"    server.middlewares.use('" + DevBase + "__test/enospc', (req, res) => {\n" +
			"      res.end('ok')\n" +
			"      server.watcher.emit('error', Object.assign(new Error('ENOSPC'), { code: 'ENOSPC' }))\n    })\n" +
			"  },\n" +
			"  async closeBundle() { fs.writeFileSync(" + strconv.Quote(closing) + ", ''); await new Promise((r) => setTimeout(r, 900)) } }\n" +
			"import react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
			"export default { plugins: [tifa(), react(), control], server: { watch: { useFsEvents: false, usePolling: false } } } // " + note + "\n"
	}
	writeTree(t, root, map[string]string{"vite.config.ts": config("one")})
	port := freePort(t)
	d, err := NewDev(root, port, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	viteModule(t, port, "__test/enospc")
	waitFile(t, closing)
	// Take the port as soon as the old server lets go of it.
	var taken net.Listener
	deadline := time.Now().Add(10 * time.Second)
	for taken == nil {
		if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			taken = l
		} else if time.Now().After(deadline) {
			t.Fatalf("the port stays in use: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(2 * time.Second) // the new server tries to listen meanwhile, and fails
	taken.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatalf("Build after the failed restart: %v", err)
	}
	writeTree(t, root, map[string]string{"vite.config.ts": config("two")})
	deadline = time.Now().Add(15 * time.Second)
	for {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%sapp/page/view.tsx", port, DevBase))
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the dev server is not back 15s after the config changed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
