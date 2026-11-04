package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/config"
)

func TestFingerprintFollowsEveryFile(t *testing.T) {
	files := map[string][]byte{"main.go": []byte("package main"), "routes.tsx": []byte("a")}
	same := map[string][]byte{"routes.tsx": []byte("a"), "main.go": []byte("package main")}
	if fingerprint(files) != fingerprint(same) {
		t.Fatal("the same files give different fingerprints")
	}
	for name, other := range map[string]map[string][]byte{
		"content": {"main.go": []byte("package main"), "routes.tsx": []byte("b")},
		"name":    {"main.go": []byte("package main"), "routes.ts": []byte("a")},
		"split":   {"main.go": []byte("package mainr"), "outes.tsx": []byte("a")},
	} {
		if fingerprint(files) == fingerprint(other) {
			t.Errorf("%s change keeps the fingerprint", name)
		}
	}
}

// Convention violations reach the browser overlay with their positions, one per line.
func TestDevAnalyzeReportsViolations(t *testing.T) {
	root, err := filepath.Abs(testApp("duppath"))
	if err != nil {
		t.Fatal(err)
	}
	p := &devPipeline{root: root}
	_, err = p.Analyze(context.Background())
	if err == nil || !strings.Contains(err.Error(), ".go:") {
		t.Fatalf("got %v, want violations with file and line", err)
	}
}

func TestDevAnalyzeIsStable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "internal", "codegen", "testdata", "shop"))
	if err != nil {
		t.Fatal(err)
	}
	// The fixture has pages, so their exports are read with the Vite of test/apps/pages.
	pages, err := filepath.Abs(filepath.Join("..", "..", "test", "apps", "pages"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	if _, err := os.Stat(filepath.Join(pages, "node_modules", "vite")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}
	front, err := bundle.NewDev(pages, 0, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer front.Close()
	p := &devPipeline{root: root, front: front}
	first, err := p.Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Analyze(context.Background())
	if err != nil || second != first || first == "" {
		t.Fatalf("fingerprints %q and %q, %v", first, second, err)
	}
}

func TestDevListensOnTheDefaultAddr(t *testing.T) {
	if got := newDevCmd().Flags().Lookup("addr").DefValue; got != config.DefaultAddr {
		t.Fatalf("--addr defaults to %q, want %q", got, config.DefaultAddr)
	}
}

// A terminal that closes sends SIGHUP, which stops tifa dev as Ctrl-C does. nohup starts a program
// with SIGHUP ignored, and then tifa dev keeps ignoring it: asking Go for the signal would turn it
// back on.
func TestDevStopSignals(t *testing.T) {
	was := signal.Ignored(syscall.SIGHUP)
	sink := make(chan os.Signal, 1)
	t.Cleanup(func() {
		signal.Stop(sink)
		if was {
			signal.Ignore(syscall.SIGHUP)
		} else {
			signal.Reset(syscall.SIGHUP)
		}
	})
	for _, tc := range []struct {
		name    string
		ignored bool
		want    []os.Signal
	}{
		{"terminal", false, []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}},
		{"nohup", true, []os.Signal{os.Interrupt, syscall.SIGTERM}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Reset would leave SIGHUP ignored when this test run was started under nohup, and
			// asking for the signal turns it on whatever it was.
			if tc.ignored {
				signal.Ignore(syscall.SIGHUP)
			} else {
				signal.Notify(sink, syscall.SIGHUP)
			}
			if got := stopSignals(); !slices.Equal(got, tc.want) {
				t.Fatalf("stopSignals() = %v, want %v", got, tc.want)
			}
		})
	}
}

// copyTestApp copies the app in dir into a new folder, with a link to its node_modules when it has
// one, and points the replace of go.mod at this repository.
func copyTestApp(t *testing.T, dir string) string {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		src := filepath.Join(dir, e.Name())
		switch e.Name() {
		case ".tifa", "bin":
		case "node_modules":
			abs, err := filepath.Abs(src)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(abs, filepath.Join(dst, e.Name())); err != nil {
				t.Fatal(err)
			}
		default:
			if e.IsDir() {
				err = os.CopyFS(filepath.Join(dst, e.Name()), os.DirFS(src))
			} else {
				var data []byte
				if data, err = os.ReadFile(src); err == nil {
					err = os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	gomod := filepath.Join(dst, "go.mod")
	data, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	data = regexp.MustCompile(`(?m)^replace github.com/liustack/tifa => .*$`).ReplaceAll(data, []byte("replace github.com/liustack/tifa => "+repo))
	if err := os.WriteFile(gomod, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// The content step scans content/ again each time, also right after the first analysis: an app
// without collections that gets a folder under content/ is told that nothing declares it.
func TestDevContentScansAFolderWithoutCollections(t *testing.T) {
	root := copyTestApp(t, filepath.Join("..", "..", "test", "apps", "api"))
	front, err := bundle.NewDev(root, 0, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer front.Close()
	p := &devPipeline{root: root, front: front}
	if _, err := p.Analyze(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "content", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Content(context.Background(), false, []string{"content/notes"}); err == nil || !strings.Contains(err.Error(), "content/notes: no collection reads this folder") {
		t.Fatalf("Content = %v, want the folder no collection reads", err)
	}
}

// A conversion that failed, the first one included, does not hold on to the Markdown it failed
// on: the next content step converts what content/ holds then.
func TestDevContentScansAgainAfterAFailedConversion(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	src := filepath.Join("..", "..", "test", "apps", "content")
	if _, err := os.Stat(filepath.Join(src, "node_modules", "vite")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/content first")
	}
	root := copyTestApp(t, src)
	post := filepath.Join(root, "content", "blog", "en", "202609", "second", "index.md")
	text, err := os.ReadFile(post)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(post, append(slices.Clip(text), "\n![x](./missing.png)\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	front, err := bundle.NewDev(root, port, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer front.Close()
	p := &devPipeline{root: root, front: front}
	ctx := context.Background()
	if _, err := p.Analyze(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Generate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Bundle(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Content(ctx, true, nil); err == nil || !strings.Contains(err.Error(), "image ./missing.png not found") {
		t.Fatalf("first Content = %v, want the missing image", err)
	}
	if err := os.WriteFile(post, text, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Content(ctx, false, []string{"content/blog/en/202609/second/index.md"}); err != nil {
		t.Fatalf("Content after the fix: %v", err)
	}
}
