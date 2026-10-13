package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"

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
			// The generated main imports tifa/server, whose dependencies go mod tidy does not
			// see because it skips .tifa. -mod=mod lets go build add their requirements.
			gobuild := exec.CommandContext(cmd.Context(), "go", "build", "-mod=mod", "-o", output, "./"+codegen.Dir)
			gobuild.Dir = root
			gobuild.Env = append(os.Environ(), "CGO_ENABLED=0")
			gobuild.Stdout, gobuild.Stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
			if err := gobuild.Run(); err != nil {
				return fmt.Errorf("go build: %w", err)
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
