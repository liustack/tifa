package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/config"
	"github.com/spf13/cobra"
)

func newBuildCmd() *cobra.Command {
	var dir, output string
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the application into a single executable",
		Long: `build checks the routing conventions, generates the main package and the TypeScript
route tree into .tifa/, bundles the browser and SSR code into .tifa/dist with the application's
Vite and compiles the executable, which embeds .tifa/dist, with CGO_ENABLED=0.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			// 1. Static analysis.
			m, err := analyzeApp(cmd, root)
			if err != nil {
				return err
			}
			// Node runs the application's Vite. It starts only for an application with pages.
			node := bundle.NewNode(root, cmd.ErrOrStderr())
			defer node.Close()
			// 2. Code generation.
			files, err := codegen.Generate(root, m, false, node.Exports)
			if err != nil {
				return err
			}
			if err := codegen.Write(root, files); err != nil {
				return err
			}
			// 3. Bundles: the browser and SSR bundles and the public files, into .tifa/dist.
			settings, err := codegen.BuildSettings(root)
			if err != nil {
				return err
			}
			_, pages := files["client.ts"]
			if err := checkTanStack(root, settings, pages); err != nil {
				return err
			}
			if err := checkReact(root, pages); err != nil {
				return err
			}
			id, err := bundle.App(node, root, filepath.Join(root, settings.Public), pages)
			if err != nil {
				return err
			}
			if err := node.Close(); err != nil {
				return fmt.Errorf("node: %w", err)
			}
			// 4. go build, which embeds .tifa/dist.
			if output == "" {
				output = defaultOutput(root, m.Module)
			}
			flags, err := buildFlags(cmd.Context(), root, settings)
			if err != nil {
				return err
			}
			if _, err := goBuild(cmd.Context(), root, output, flags, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return fmt.Errorf("go build: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "built %s (build %s)\n", output, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.Flags().StringVarP(&output, "output", "o", "", "executable path (default bin/<last element of the module path>)")
	return cmd
}

// checkTanStack checks the TanStack Router of an app with pages, see bundle.CheckTanStack, unless
// the project file sets build.skip_tanstack_check.
func checkTanStack(root string, settings config.Build, pages bool) error {
	if !pages || settings.SkipTanStackCheck {
		return nil
	}
	return bundle.CheckTanStack(root)
}

// checkReact checks the React of an app with pages, see bundle.CheckReact.
func checkReact(root string, pages bool) error {
	if !pages {
		return nil
	}
	return bundle.CheckReact(root)
}

// analyzeApp runs the static analysis and prints each convention violation on its own line.
func analyzeApp(cmd *cobra.Command, root string) (*analyze.Manifest, error) {
	m, err := analyze.Analyze(root)
	var violations analyze.Errors
	if errors.As(err, &violations) {
		for _, v := range violations {
			fmt.Fprintln(cmd.ErrOrStderr(), v)
		}
		return nil, fmt.Errorf("found %d convention violation(s)", len(violations))
	}
	return m, err
}

// goBuild compiles the generated main of the application at root into output with
// CGO_ENABLED=0 and the given flags of go build, see buildFlags. It returns what go
// build printed on stderr, which also goes to stderr. go build leaves go.mod and go.sum as they are.
func goBuild(ctx context.Context, root, output string, flags []string, stdout, stderr io.Writer) (string, error) {
	args := append([]string{"build", "-o", output}, flags...)
	gobuild := exec.CommandContext(ctx, "go", append(args, "./"+codegen.Dir)...)
	gobuild.Dir = root
	gobuild.Env = append(os.Environ(), "CGO_ENABLED=0")
	var printed bytes.Buffer
	gobuild.Stdout = stdout
	gobuild.Stderr = io.MultiWriter(stderr, &printed)
	if err := gobuild.Run(); err != nil {
		return printed.String(), fmt.Errorf("%w%s", err, missingDependencyHint(printed.String()))
	}
	return printed.String(), nil
}

// buildFlags are the flags of go build in tifa build: those of ldflags unless build.debug_info
// keeps the DWARF debug info. It runs no go env when it keeps it.
func buildFlags(ctx context.Context, root string, settings config.Build) ([]string, error) {
	if settings.DebugInfo {
		return nil, nil
	}
	goflags, err := goEnvFlags(ctx, root)
	if err != nil {
		return nil, err
	}
	return ldflags(goflags)
}

// goEnvFlags returns GOFLAGS as the go command sees it in root: go env also reads it from the
// file go env -w writes.
func goEnvFlags(ctx context.Context, root string) (string, error) {
	goenv := exec.CommandContext(ctx, "go", "env", "GOFLAGS")
	goenv.Dir = root
	out, err := goenv.Output()
	if err != nil {
		return "", fmt.Errorf("go env GOFLAGS: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ldflags returns -ldflags with -w, which leaves the DWARF debug info out of the program. The
// program still has the line table of the Go runtime, which panics, runtime.Caller and pprof read.
// For each package, go build uses the last -ldflags that matches it, and the command line comes
// after GOFLAGS, so the flags of the last -ldflags in goflags come before -w. It returns none when
// those flags already have -w or -s, or when an -ldflags of goflags is for a package pattern, which
// only the go command can match against the main package.
func ldflags(goflags string) ([]string, error) {
	words, err := splitQuoted(goflags)
	if err != nil {
		return nil, fmt.Errorf("GOFLAGS: %w", err)
	}
	user := ""
	for _, word := range words {
		value, ok := strings.CutPrefix(trimDashes(word), "ldflags=")
		if !ok {
			continue
		}
		user = strings.TrimSpace(value)
		if user != "" && !strings.HasPrefix(user, "-") {
			return nil, nil // pattern=flags
		}
	}
	flags, err := splitQuoted(user)
	if err != nil {
		return nil, fmt.Errorf("GOFLAGS: -ldflags: %w", err)
	}
	for _, flag := range flags {
		name, _, _ := strings.Cut(trimDashes(flag), "=")
		if name == "w" || name == "s" {
			return nil, nil
		}
	}
	if user == "" {
		return []string{"-ldflags=-w"}, nil
	}
	return []string{"-ldflags=" + user + " -w"}, nil
}

// trimDashes takes the one or two dashes off a flag, as Go's flag package reads -x and --x alike.
func trimDashes(flag string) string {
	return strings.TrimPrefix(strings.TrimPrefix(flag, "-"), "-")
}

// splitQuoted splits s at spaces as the go command splits GOFLAGS and -ldflags: a word that
// starts with a single or double quote runs to the next such quote, without escapes.
func splitQuoted(s string) ([]string, error) {
	var words []string
	for {
		s = strings.TrimLeft(s, " \t\n\r")
		if s == "" {
			return words, nil
		}
		if quote := s[0]; quote == '"' || quote == '\'' {
			end := strings.IndexByte(s[1:], quote)
			if end < 0 {
				return nil, fmt.Errorf("unterminated %c string", quote)
			}
			words = append(words, s[1:1+end])
			s = s[2+end:]
			continue
		}
		end := strings.IndexAny(s, " \t\n\r")
		if end < 0 {
			end = len(s)
		}
		words = append(words, s[:end])
		s = s[end:]
	}
}

// missingDependencyHint tells what to run when go build misses requirements of the generated
// main. An application that lists cmd/tifa as a tool has all of them after go mod tidy (see deps.go).
func missingDependencyHint(stderr string) string {
	for _, marker := range []string{"missing go.sum entry", "no required module provides", "updates to go.mod needed"} {
		if strings.Contains(stderr, marker) {
			return "\nthe application's go.mod lacks dependencies of the generated main, list the tifa command as a tool:\n\tgo get -tool github.com/liustack/tifa/cmd/tifa\n\tgo mod tidy"
		}
	}
	return ""
}

// defaultOutput is where tifa build writes the executable without -o: bin/ of the application,
// named after the last element of the module path, as Go projects keep their binaries.
func defaultOutput(root, module string) string {
	return filepath.Join(root, "bin", path.Base(module))
}
