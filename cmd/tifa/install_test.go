package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A fresh clone has a package.json and no node_modules. The message names the install command.
func TestCheckInstalledNamesTheInstallCommand(t *testing.T) {
	for _, tc := range []struct {
		pnpm bool
		want string
	}{{true, "\n\tpnpm install"}, {false, "\n\tnpm install"}} {
		withPnpm(t, tc.pnpm)
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "package.json"), "{}")
		err := checkInstalled(root)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "not installed") {
			t.Errorf("pnpm=%v: got %v, want a message ending in %q", tc.pnpm, err, tc.want)
		}
	}
}

// An app without pages has no package.json and needs no Node. An installed app passes, also when
// Vite sits in a node_modules above the app, as in a workspace.
func TestCheckInstalledLeavesOtherAppsAlone(t *testing.T) {
	if err := checkInstalled(t.TempDir()); err != nil {
		t.Errorf("no package.json: %v", err)
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), "{}")
	writeFile(t, filepath.Join(root, "node_modules", "vite", "package.json"), `{"version":"1.0.0"}`)
	if err := checkInstalled(root); err != nil {
		t.Errorf("installed: %v", err)
	}
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "node_modules", "vite", "package.json"), `{"version":"1.0.0"}`)
	writeFile(t, filepath.Join(ws, "app", "package.json"), "{}")
	if err := checkInstalled(filepath.Join(ws, "app")); err != nil {
		t.Errorf("workspace: %v", err)
	}
}

// tifa build and tifa dev stop on the missing packages before they analyze or start anything.
func TestBuildAndDevStopOnMissingPackages(t *testing.T) {
	withPnpm(t, true)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), "{}")
	for name, run := range map[string]func() error{
		"build": func() error {
			c := newBuildCmd()
			c.SetArgs([]string{"--dir", root})
			return c.Execute()
		},
		"dev": func() error {
			c := newDevCmd()
			c.SetArgs([]string{"--dir", root, "--addr", "127.0.0.1:0"})
			return c.Execute()
		},
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), "\n\tpnpm install") {
			t.Errorf("%s: got %v", name, err)
		}
	}
}
