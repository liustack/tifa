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
			if _, err := goBuild(cmd.Context(), root, output, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
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
// CGO_ENABLED=0. It returns what go build printed on stderr, which also goes to stderr.
// go build leaves go.mod and go.sum as they are.
func goBuild(ctx context.Context, root, output string, stdout, stderr io.Writer) (string, error) {
	gobuild := exec.CommandContext(ctx, "go", "build", "-o", output, "./"+codegen.Dir)
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
