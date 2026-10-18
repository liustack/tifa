package codegen

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// enginePGO is a CPU profile of the example shop's pages rendering in warm VMs on linux/amd64.
// Nearly all of it is the JavaScript engine, libc and the Go runtime, which every page runs
// through. See "The engine profile" in docs/testing.md.
//
//go:embed engine.pgo
var enginePGO []byte

// pgoFile is the profile go build optimizes the main package in Dir with: the go command's
// default -pgo=auto takes default.pgo from the main package's directory.
const pgoFile = "default.pgo"

// profile returns the profile build.pgo selects for the app at root, nil for none: off is none,
// a file is that file relative to root, and auto is the app's default.pgo, or else the engine
// profile when the app has pages to render.
func profile(root, setting string, pages bool) ([]byte, error) {
	switch setting {
	case "off":
		return nil, nil
	case "auto":
		data, err := os.ReadFile(filepath.Join(root, pgoFile))
		if errors.Is(err, fs.ErrNotExist) {
			if pages {
				return enginePGO, nil
			}
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("build.pgo: %w", err)
		}
		return data, nil
	}
	if filepath.IsAbs(setting) {
		return nil, fmt.Errorf("build.pgo: %s is an absolute path, write the path relative to the app root", setting)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(setting)))
	if err != nil {
		return nil, fmt.Errorf("build.pgo: %w", err)
	}
	return data, nil
}
