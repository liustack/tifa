package main

import (
	"slices"
	"strings"
	"testing"
)

func TestGoBuildArgsKeepTheCallersModFlag(t *testing.T) {
	for _, tc := range []struct {
		goflags string
		mod     bool
	}{
		{"", true},
		{"-trimpath", true},
		{"-mod=readonly", false},
		{"-trimpath -mod=vendor", false},
		{"--mod=readonly", false},
	} {
		args := goBuildArgs(tc.goflags, "out")
		if got := slices.Contains(args, "-mod=mod"); got != tc.mod {
			t.Errorf("GOFLAGS=%q: args %q, want -mod=mod %v", tc.goflags, args, tc.mod)
		}
	}
}

func TestMissingDependencyHint(t *testing.T) {
	for _, stderr := range []string{
		"main.go:4:2: missing go.sum entry for module providing package github.com/spf13/cobra",
		"main.go:4:2: no required module provides package github.com/spf13/cobra; to add it:",
		"go: updates to go.mod needed; to update it:\n\tgo mod tidy",
	} {
		if hint := missingDependencyHint(stderr); !strings.Contains(hint, "go get github.com/liustack/tifa/server") {
			t.Errorf("hint for %q = %q", stderr, hint)
		}
	}
	if hint := missingDependencyHint("main.go:1: syntax error"); hint != "" {
		t.Errorf("hint for a syntax error = %q", hint)
	}
}
