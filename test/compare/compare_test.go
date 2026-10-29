package compare_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// TestPagesShowTheSameText builds the shop as a tifa program and as a Next.js and a TanStack Start
// app, and checks that the six workbench pages show the same title and text, with as many tables
// rows, links, SVGs, buttons, times and photos. Build output and node_modules stay in the folders
// of the apps (git ignores them), so a second run only builds what changed.
//
//	TIFA_COMPARE=1 go test -count=1 -v ./test/compare
//
// TIFA_COMPARE_SKIP_BUILD=1 reuses the last build. The test needs go, node and pnpm and is skipped
// without them. It is not part of CI.
func TestPagesShowTheSameText(t *testing.T) {
	if os.Getenv("TIFA_COMPARE") != "1" {
		t.Skip("builds three apps with Node, set TIFA_COMPARE=1 to run it")
	}
	if why := compare.MissingTools(); why != "" {
		t.Skip(why)
	}
	env, err := compare.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if os.Getenv("TIFA_COMPARE_SKIP_BUILD") != "1" {
		var log strings.Builder
		if err := env.Prepare(ctx, &log); err != nil {
			t.Fatalf("%v\n%s", err, &log)
		}
	}

	views := map[string][]compare.View{}
	var names []string
	for _, target := range env.Targets() {
		if target.Name == "nextjs-jitless" {
			continue // the same app and the same HTML as nextjs
		}
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range compare.Pages {
			body, err := compare.Fetch(server.Base, page)
			if err != nil {
				t.Errorf("%s: %v", target.Name, err)
				views[target.Name] = append(views[target.Name], compare.View{})
				continue
			}
			views[target.Name] = append(views[target.Name], compare.ParseView(body))
		}
		server.Stop()
		names = append(names, target.Name)
	}
	tifa := views["tifa"]
	for _, name := range names[1:] {
		for i, page := range compare.Pages {
			if len(views[name]) <= i || len(tifa) <= i {
				continue
			}
			if diff := compare.Diff(tifa[i], views[name][i]); diff != "" {
				t.Errorf("%s, %s differs from tifa:\n%s", page.Name, name, diff)
			}
		}
	}
	for i, page := range compare.Pages {
		t.Logf("%-14s %2d text nodes, tifa %d bytes, Next.js %d, Start %d", page.Name, len(tifa[i].Text), tifa[i].Bytes, views["nextjs"][i].Bytes, views["start"][i].Bytes)
	}
}
