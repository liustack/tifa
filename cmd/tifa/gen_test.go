package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gen writes the generated code into .tifa/ and builds nothing.
func TestGenWritesTheCodeWithoutBuilding(t *testing.T) {
	dir := apiApp(t)
	out, stderr, err := run(t, "gen", "--dir", dir)
	if err != nil {
		t.Fatalf("gen: %v\nstdout:\n%s\nstderr:\n%s", err, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tifa", "main.go")); err != nil {
		t.Errorf("gen did not generate .tifa: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tifa", "dist")); !os.IsNotExist(err) {
		t.Errorf("gen built the app: .tifa/dist: %v", err)
	}
	// A second run in an app that has .tifa/ already works the same.
	if out, stderr, err := run(t, "gen", "--dir", dir); err != nil {
		t.Fatalf("second gen: %v\nstdout:\n%s\nstderr:\n%s", err, out, stderr)
	}
}

// gen stops at a convention violation and generates nothing.
func TestGenFailsOnAViolation(t *testing.T) {
	dir := apiApp(t)
	writeFile(t, filepath.Join(dir, "broken.go"), "package apitest\n\nvar _ int = \"text\"\n")
	_, stderr, err := run(t, "gen", "--dir", dir)
	if err == nil {
		t.Fatal("gen passed an app with a type error")
	}
	if !strings.Contains(stderr, "broken.go:3:") {
		t.Errorf("stderr does not name the error:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tifa", "main.go")); !os.IsNotExist(err) {
		t.Errorf("gen generated code for a broken app: %v", err)
	}
}
