package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// A real React page uses Temporal both at module load and during rendering.
// Keep the date fixed so the server output is also suitable for hydration.
func TestTemporalPage(t *testing.T) {
	skipUnlessEnabled(t)
	dir := devApp(t)
	page := `const day = Temporal.PlainDate.from('2026-10-22')
export default function Home() {
  return <time>{day.add({days: 1}).toLocaleString('en-US', {dateStyle: 'long'})}</time>
}
`
	if err := os.WriteFile(filepath.Join(dir, "app", "route.tsx"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	base, _, _ := start(t, buildAt(t, dir))
	expectBody(t, base+"/", "<time>October 23, 2026</time>")
}
