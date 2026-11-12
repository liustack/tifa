package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A copy of test/apps/pages gets two pages whose Load data changes on every request: /tally shows
// only a title and carries a count it does not show, /count shows its count. Each request renders
// both again. tifa.Stats counts by route the renders of /tally after the first as unchanged, since
// its page shows the same, and none of /count, whose page changed. The real bundle writes the page,
// so the part compared is the one serve in js/runtime/server.tsx writes before the router's scripts.
func TestMemoCountsThePagesThatRenderUnchanged(t *testing.T) {
	skipUnlessEnabled(t)
	dir := copyApp(t, "pages")
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app/tally/route.go", `package tally

import (
	"sync/atomic"

	"github.com/liustack/tifa"
)

var visits atomic.Int64

type Tally struct {
	Title  string `+"`json:\"title\"`"+`
	Visits int64  `+"`json:\"visits\"`"+`
}

func Load(c *tifa.Ctx) (Tally, error) { return Tally{Title: "Tally", Visits: visits.Add(1)}, nil }
`)
	write("app/tally/page.tsx", `import { page } from './route.gen'

export default function Tally() {
  return <h1 id="tally">{page.useLoaderData().title}</h1>
}
`)
	write("app/count/route.go", `package count

import (
	"sync/atomic"

	"github.com/liustack/tifa"
)

var visits atomic.Int64

type Count struct {
	Visits int64 `+"`json:\"visits\"`"+`
}

func Load(c *tifa.Ctx) (Count, error) { return Count{Visits: visits.Add(1)}, nil }
`)
	write("app/count/page.tsx", `import { page } from './route.gen'

export default function Count() {
  return <h1 id="count">{page.useLoaderData().visits}</h1>
}
`)
	write("app/memo/route.go", `package memo

import "github.com/liustack/tifa"

func GET(c *tifa.Ctx) error { return c.JSON(200, tifa.Stats().Memo.Routes) }
`)
	exe := buildAt(t, dir)
	base, _, _ := start(t, exe)

	bodies := map[string]bool{}
	for range 3 {
		for _, path := range []string{"/tally", "/count"} {
			status, header, body := get(t, base+path)
			if status != 200 || header.Get("X-Tifa-Render") != "vm" {
				t.Fatalf("%s: %d %s, want a render", path, status, header.Get("X-Tifa-Render"))
			}
			bodies[body] = true
		}
	}
	if len(bodies) != 6 {
		t.Fatalf("%d different pages of 6, want the data in each", len(bodies))
	}
	status, _, body := get(t, base+"/memo")
	var routes map[string]struct{ Hits, Misses, Shared, Unchanged uint64 }
	if err := json.Unmarshal([]byte(body), &routes); status != 200 || err != nil {
		t.Fatalf("/memo: %d %v %s", status, err, body)
	}
	if r := routes["/tally"]; r.Misses != 3 || r.Unchanged != 2 || r.Hits != 0 {
		t.Errorf("/tally %+v, want 3 misses and 2 unchanged", r)
	}
	if r := routes["/count"]; r.Misses != 3 || r.Unchanged != 0 || r.Hits != 0 {
		t.Errorf("/count %+v, want 3 misses and none unchanged", r)
	}
}
