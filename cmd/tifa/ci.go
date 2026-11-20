package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// ciPath is the workflow that tifa new writes and tifa agents writes once when it is missing.
	ciPath = ".github/workflows/ci.yml"
	// defaultPnpmVersion is the pnpm major version for pnpm/action-setup when package.json has no
	// packageManager. The action fails without either.
	defaultPnpmVersion = "10"
)

// planCI returns the workflow for an existing app at root, or nil when the app has a workflow
// already (ci.yml or any other: a second one would run the checks twice). It never replaces a
// file. A package manager that the workflow has no steps for gets a note and no file.
func planCI(root string, app newApp, notes []string) (*agentChange, []string, error) {
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ciPath))); err == nil {
		return nil, notes, nil
	}
	existing, err := os.ReadDir(filepath.Join(root, ".github", "workflows"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, notes, err
	}
	for _, e := range existing {
		if ext := filepath.Ext(e.Name()); !e.IsDir() && (ext == ".yml" || ext == ".yaml") {
			return nil, notes, nil
		}
	}
	pkg, err := os.ReadFile(filepath.Join(root, "package.json"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, notes, err
	default:
		app.Node = true
		if app.Run != "pnpm" && app.Run != "npm run" {
			return nil, append(notes, fmt.Sprintf("no %s: the workflow has steps for pnpm and npm only, so write one for %s", ciPath, strings.Fields(app.Install)[0])), nil
		}
		var p struct {
			PackageManager string `json:"packageManager"`
		}
		if err := json.Unmarshal(pkg, &p); err != nil {
			return nil, notes, fmt.Errorf("package.json: %w", err)
		}
		// pnpm/action-setup reads the version from packageManager, and needs one of its own without it.
		if !strings.HasPrefix(p.PackageManager, "pnpm@") {
			app.PnpmVersion = defaultPnpmVersion
		}
	}
	text, err := renderFile(newapp, "newapp/github-ci.yml.tmpl", app)
	if err != nil {
		return nil, notes, err
	}
	return &agentChange{rel: ciPath, content: text, what: "created"}, notes, nil
}
