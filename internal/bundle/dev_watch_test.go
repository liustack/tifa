package bundle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
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
