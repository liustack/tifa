package bundle

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

// tailwindSyntax finds what makes a stylesheet a Tailwind one: an import of tailwindcss, or one of
// the at-rules only Tailwind knows. Other stylesheets go to esbuild as they are, without Node.
var tailwindSyntax = regexp.MustCompile(`@import\s+(?:url\(\s*)?["']tailwindcss(?:/[^"']*)?["']|` +
	`@(?:tailwind|apply|reference|config|plugin|theme|utility|variant|custom-variant|source)\b`)

func usesTailwind(css []byte) bool { return tailwindSyntax.Match(css) }

// tailwindInstall is what the error for a missing Tailwind tells to run.
const tailwindInstall = "pnpm add -D tailwindcss @tailwindcss/cli"

// tailwindPlugin compiles the stylesheets of the browser bundle that use Tailwind with the
// application's own Tailwind packages, which run on Node. Tailwind scans the files under root for
// class names, so every build compiles them again. spent adds up the time Tailwind took.
func tailwindPlugin(root string, spent *atomic.Int64) api.Plugin {
	return api.Plugin{Name: "tifa-tailwind", Setup: func(b api.PluginBuild) {
		b.OnLoad(api.OnLoadOptions{Filter: `\.css$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
			css, err := os.ReadFile(args.Path)
			if err != nil || !usesTailwind(css) {
				// esbuild loads the file itself and reports a read error.
				return api.OnLoadResult{}, nil
			}
			start := time.Now()
			out, err := tailwind(root, args.Path, css)
			spent.Add(int64(time.Since(start)))
			if err != nil {
				return api.OnLoadResult{}, err
			}
			return api.OnLoadResult{Contents: &out, Loader: api.LoaderCSS, ResolveDir: filepath.Dir(args.Path)}, nil
		})
	}}
}

// tailwindScript compiles one stylesheet on Node, see the comment at its top.
//
//go:embed tailwind.cjs
var tailwindScript string

// tailwind compiles the stylesheet file with content css by running tailwindScript on Node with the
// Tailwind packages of the application. Tailwind scans root for class names, leaving out tifa's
// output in .tifa and bin, whose old class names would never go away.
func tailwind(root, file string, css []byte) (string, error) {
	// Tailwind compares the paths it scans after following symbolic links, and so does esbuild
	// with the path of file.
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	name := file
	if rel, err := filepath.Rel(real, file); err == nil && filepath.IsLocal(rel) {
		name = filepath.ToSlash(rel)
	}
	cli, err := tailwindPackage(real)
	if err != nil {
		return "", err
	}
	if cli == "" {
		return "", fmt.Errorf("%s uses Tailwind CSS, which is not installed in the application, install it with:\n\t%s", name, tailwindInstall)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return "", fmt.Errorf("%s uses Tailwind CSS, which runs on Node, and node is not on PATH", name)
	}
	source := string(css)
	for _, dir := range []string{".tifa", "bin"} {
		source += "\n@source not " + cssString(filepath.ToSlash(filepath.Join(real, dir))) + ";"
	}
	input, err := json.Marshal(map[string]string{"cli": cli, "root": real, "file": file, "css": source + "\n"})
	if err != nil {
		return "", err
	}
	cmd := exec.Command(node, "-e", tailwindScript)
	cmd.Dir = real
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s: %s", name, msg)
		}
		return "", fmt.Errorf("%s: tailwind: %w", name, err)
	}
	return stdout.String(), nil
}

// tailwindPackage finds the package.json of @tailwindcss/cli in the node_modules of root or of a
// directory above it, the way Node finds packages, and returns its real path, next to which Node
// finds the Tailwind packages the CLI uses. It returns "" when no directory has it.
func tailwindPackage(root string) (string, error) {
	for dir := root; ; dir = filepath.Dir(dir) {
		pkg, err := filepath.EvalSymlinks(filepath.Join(dir, "node_modules", "@tailwindcss", "cli", "package.json"))
		if err == nil {
			return pkg, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if filepath.Dir(dir) == dir {
			return "", nil
		}
	}
}

// cssString quotes s as a CSS string.
func cssString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\a `).Replace(s) + `"`
}

// TailwindTime is how long Tailwind ran during the last Build, zero when no stylesheet uses it.
func (d *Dev) TailwindTime() time.Duration { return time.Duration(d.tailwind.Load()) }
