package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TypeScriptCompiler returns the path of tsc, the JavaScript file that node runs, in the
// typescript package the app at root installs. It looks the way Node resolves a package, so it
// finds the package behind the links of pnpm and in the node_modules of a folder above the app.
func TypeScriptCompiler(root string) (string, error) {
	dir, _, err := findPackage(root, "typescript")
	if err != nil {
		return "", err
	}
	file := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	var meta struct {
		Bin map[string]string `json:"bin"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("%s: %w", file, err)
	}
	tsc, ok := meta.Bin["tsc"]
	if !ok {
		return "", fmt.Errorf("%s has no tsc in its bin", file)
	}
	return filepath.Join(dir, filepath.FromSlash(tsc)), nil
}
