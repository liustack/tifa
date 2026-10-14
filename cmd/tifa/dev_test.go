package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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
	p := &devPipeline{root: root}
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
