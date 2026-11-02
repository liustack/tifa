package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/liustack/tifa/internal/bundle"
)

// checkInstalled fails with the install command to run when the app at root has a package.json
// and Vite does not resolve from it, which is how a fresh clone looks. An app without a
// package.json has no pages and needs no Node, so it passes.
func checkInstalled(root string) error {
	if _, err := os.Stat(filepath.Join(root, "package.json")); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	ok, err := bundle.ViteInstalled(root)
	if err != nil || ok {
		return err
	}
	install, _ := packageManager()
	return fmt.Errorf("the npm packages of this app are not installed (Vite is not in node_modules). Install them and run this again:\n\t%s", install)
}
