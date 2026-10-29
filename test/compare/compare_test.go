package compare_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// The tests build three apps and run them, so they are off unless TIFA_COMPARE=1. They need go,
// node and pnpm and are skipped without them. TIFA_COMPARE_SKIP_BUILD=1 uses what the last run
// built. None of this is part of CI.
//
//	TIFA_COMPARE=1 go test -count=1 -v ./test/compare

func setup(t *testing.T) compare.Env {
	t.Helper()
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
	return env
}

var (
	shopOnce, appsOnce sync.Once
	shopErr, appsErr   error
	shopLog, appsLog   strings.Builder
)

// buildShop builds bin/shop once for all the tests.
func buildShop(t *testing.T, env compare.Env) {
	t.Helper()
	if os.Getenv("TIFA_COMPARE_SKIP_BUILD") == "1" {
		return
	}
	shopOnce.Do(func() { shopErr = env.BuildShop(context.Background(), &shopLog) })
	if shopErr != nil {
		t.Fatalf("%v\n%s", shopErr, &shopLog)
	}
}

// buildApps builds the shop and the two Node apps once for all the tests.
func buildApps(t *testing.T, env compare.Env) {
	t.Helper()
	if os.Getenv("TIFA_COMPARE_SKIP_BUILD") == "1" {
		return
	}
	appsOnce.Do(func() { appsErr = env.Prepare(context.Background(), &appsLog) })
	if appsErr != nil {
		t.Fatalf("%v\n%s", appsErr, &appsLog)
	}
}

// TestDomainMatchesGo checks that the TypeScript port of the shop's domain code computes what the
// Go code returns. For each of the six pages it compares, field by field, the JSON that the Go
// Layout and Load functions answer with the JSON that Node makes from the port. When the Go code
// of the shop changes and the port does not, this fails and names the first fields that differ.
// It needs no build of the two Node apps: Node 22.18 and later run the port as it is.
func TestDomainMatchesGo(t *testing.T) {
	env := setup(t)
	if why := compare.NodeRunsTypeScript(); why != "" {
		t.Skip(why)
	}
	buildShop(t, env)
	ctx := context.Background()
	want, err := env.GoData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.NodeData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantAny, gotAny := toAny(t, want), toAny(t, got)
	diffs := compare.DiffData(wantAny, gotAny, 20)
	for _, d := range diffs {
		t.Error(d)
	}
	if len(diffs) > 0 {
		t.Error("the TypeScript port in test/compare/domain no longer matches the Go code of examples/shop: port the change")
	}
	for _, p := range compare.Pages {
		if len(want[p.URL]) == 0 || len(got[p.URL]) != len(want[p.URL]) {
			t.Errorf("%s: Go answered %d routes, Node %d", p.URL, len(want[p.URL]), len(got[p.URL]))
		}
	}
}

func toAny(t *testing.T, v compare.PageData) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPagesShowTheSameText builds the shop as a tifa program and as a Next.js and a TanStack Start
// app, and checks that the six workbench pages show the same title and text, with as many table
// rows, links, SVGs, buttons, times and photos.
func TestPagesShowTheSameText(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()

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
