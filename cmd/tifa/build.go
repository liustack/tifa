package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
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
route tree into .tifa/, bundles the browser and SSR code into .tifa/dist and compiles the
executable, which embeds .tifa/dist, with CGO_ENABLED=0.`,
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
			// 2. Code generation.
			files, err := codegen.Generate(root, m, false)
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
			id, err := bundle.App(root, filepath.Join(root, settings.Public), pages)
			if err != nil {
				return err
			}
			// 4. go build, which embeds .tifa/dist.
			if output == "" {
				output = filepath.Join(root, "dist", path.Base(m.Module))
			}
			gobuild := exec.CommandContext(cmd.Context(), "go", goBuildArgs(os.Getenv("GOFLAGS"), output)...)
			gobuild.Dir = root
			gobuild.Env = append(os.Environ(), "CGO_ENABLED=0")
			var stderr bytes.Buffer
			gobuild.Stdout = cmd.OutOrStdout()
			gobuild.Stderr = io.MultiWriter(cmd.ErrOrStderr(), &stderr)
			if err := gobuild.Run(); err != nil {
				return fmt.Errorf("go build: %w%s", err, missingDependencyHint(stderr.String()))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "built %s (build %s)\n", output, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.Flags().StringVarP(&output, "output", "o", "", "executable path (default dist/<last element of the module path>)")
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

// goBuildArgs returns the go build arguments. The generated main imports tifa/server, whose
// dependencies go mod tidy does not see because it skips .tifa, so by default -mod=mod lets go
// build add their requirements. A -mod the caller set in GOFLAGS wins, so a -mod=readonly CI
// build leaves go.mod and go.sum untouched.
func goBuildArgs(goflags, output string) []string {
	args := []string{"build"}
	if !slices.ContainsFunc(strings.Fields(goflags), func(f string) bool {
		return strings.HasPrefix(f, "-") && strings.HasPrefix(strings.TrimLeft(f, "-"), "mod=")
	}) {
		args = append(args, "-mod=mod")
	}
	return append(args, "-o", output, "./"+codegen.Dir)
}

// missingDependencyHint tells what to run when go build, held to the module files by the
// caller's -mod, misses requirements of tifa/server.
func missingDependencyHint(stderr string) string {
	for _, marker := range []string{"missing go.sum entry", "no required module provides", "updates to go.mod needed"} {
		if strings.Contains(stderr, marker) {
			return "\nthe application's go.mod lacks dependencies of the generated main, add them with:\n\tgo get github.com/liustack/tifa/server"
		}
	}
	return ""
}
