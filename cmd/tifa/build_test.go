package main

import (
	"strings"
	"testing"
)

func TestMissingDependencyHint(t *testing.T) {
	for _, stderr := range []string{
		"main.go:4:2: missing go.sum entry for module providing package github.com/spf13/cobra",
		"main.go:4:2: no required module provides package github.com/spf13/cobra; to add it:",
		"go: updates to go.mod needed; to update it:\n\tgo mod tidy",
	} {
		if hint := missingDependencyHint(stderr); !strings.Contains(hint, "go get -tool github.com/liustack/tifa/cmd/tifa") {
			t.Errorf("hint for %q = %q", stderr, hint)
		}
	}
	if hint := missingDependencyHint("main.go:1: syntax error"); hint != "" {
		t.Errorf("hint for a syntax error = %q", hint)
	}
}
