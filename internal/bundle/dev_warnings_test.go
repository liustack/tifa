package bundle

import (
	"context"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The warnings of the tifa plugin in the last build are what Warnings returns: each build replaces
// the ones before, and a warning of Vite or Rolldown is not among them.
func TestDevListsThePluginWarnings(t *testing.T) {
	root := devApp(t)
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	build := func() []string {
		t.Helper()
		if _, err := d.Build(context.Background(), filepath.Join(root, "public"), nil, true); err != nil {
			t.Fatalf("Build: %v", err)
		}
		return d.Warnings()
	}
	if got := build(); len(got) != 0 {
		t.Fatalf("the first build has warnings: %q", got)
	}
	writeTree(t, root, map[string]string{
		"app/page/view.tsx": "import './cycle'\nexport default function View({ name }: { name: string }) {\n" +
			"  return <p>{name + import.meta.env.VITE_SHOP_KEY}</p>\n}\n",
	})
	// An import cycle: Rolldown may say so on its own, and the plugin does not pass it on.
	writeTree(t, root, map[string]string{
		"app/page/cycle.ts":  "import { b } from './cycle2'\nexport const a = () => b\n",
		"app/page/cycle2.ts": "import { a } from './cycle'\nexport const b = () => a\n",
	})
	got := build()
	if len(got) != 1 || !strings.HasPrefix(got[0], "app/page/view.tsx:3:") || !strings.Contains(got[0], "import.meta.env.VITE_SHOP_KEY is fixed when the build runs") || !strings.HasSuffix(got[0], "(warning)") {
		t.Fatalf("after the read: %q", got)
	}
	if again := build(); !slices.Equal(again, got) {
		t.Fatalf("a build of the same code gives %q, want %q", again, got)
	}
	writeTree(t, root, map[string]string{
		"app/page/view.tsx": "export default function View({ name }: { name: string }) {\n  return <p>{name}</p>\n}\n",
	})
	if got := build(); len(got) != 0 {
		t.Fatalf("after the read is gone: %q", got)
	}
}
