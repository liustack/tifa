package bundle

import (
	"bytes"
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
// application's own Tailwind CLI, which runs on Node. Tailwind scans the files under root for class
// names, so every build compiles them again. spent adds up the time the CLI took.
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

// tailwind runs the Tailwind CLI on the stylesheet file with content css. The CLI reads it from
// stdin in the stylesheet's directory, so its imports resolve from there, and scans root for class
// names, leaving out tifa's output in .tifa and bin, whose old class names would never go away.
// --optimize flattens and prefixes the CSS for the browsers Tailwind supports.
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
	cli, err := tailwindCLI(root)
	if err != nil {
		return "", err
	}
	if cli == "" {
		return "", fmt.Errorf("%s uses Tailwind CSS, which is not installed in the application, install it with:\n\t%s", name, tailwindInstall)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return "", fmt.Errorf("%s uses Tailwind CSS, whose CLI runs on Node, and node is not on PATH", name)
	}
	var input bytes.Buffer
	input.Write(css)
	for _, dir := range []string{".tifa", "bin"} {
		fmt.Fprintf(&input, "\n@source not %s;", cssString(filepath.ToSlash(filepath.Join(real, dir))))
	}
	input.WriteString("\n")
	cmd := exec.Command(node, cli, "--input", "-", "--cwd", root, "--optimize", "--silent")
	cmd.Dir = filepath.Dir(file)
	cmd.Stdin = &input
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := tailwindError(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s: %s", name, msg)
		}
		return "", fmt.Errorf("%s: tailwindcss: %w", name, err)
	}
	return stdout.String(), nil
}

// ansi matches the color codes the Tailwind CLI writes whether or not stderr is a terminal.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// tailwindError takes the message out of the box the Tailwind CLI draws around an error:
//
//	Error:
//	┌
//	│ Error: Cannot apply unknown utility class `x`
//	└
func tailwindError(stderr string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(ansi.ReplaceAllString(stderr, "")), "\n") {
		line = strings.TrimSpace(line)
		if line == "Error:" || line == "┌" || line == "└" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "│"); ok {
			line = strings.TrimSpace(rest)
		}
		lines = append(lines, strings.TrimPrefix(line, "Error: "))
	}
	return strings.Join(lines, "\n")
}

// tailwindCLI finds the script of the @tailwindcss/cli package in the node_modules of root or of a
// directory above it, the way Node finds packages. It returns "" when none has it.
func tailwindCLI(root string) (string, error) {
	for dir := root; ; dir = filepath.Dir(dir) {
		pkg := filepath.Join(dir, "node_modules", "@tailwindcss", "cli")
		data, err := os.ReadFile(filepath.Join(pkg, "package.json"))
		if err == nil {
			var manifest struct {
				Bin map[string]string `json:"bin"`
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return "", fmt.Errorf("%s: %w", pkg, err)
			}
			if manifest.Bin["tailwindcss"] == "" {
				return "", fmt.Errorf("%s has no tailwindcss command", pkg)
			}
			return filepath.Join(pkg, filepath.FromSlash(manifest.Bin["tailwindcss"])), nil
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

// TailwindTime is how long the Tailwind CLI ran during the last Build, zero when no stylesheet
// uses Tailwind.
func (d *Dev) TailwindTime() time.Duration { return time.Duration(d.tailwind.Load()) }
