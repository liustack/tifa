package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Run every check of the application and stop at the first one that fails",
		Long: `check runs, in this order: the analysis of the routing conventions, the code generation
into .tifa/, go vet ./..., go test ./... and the TypeScript type check. It stops at the first
step that fails, names it in the error and exits with a non-zero status. The type check runs
the tsc of the typescript package the application installs, on Node, and is skipped for an
application without pages, which needs no Node. Go runs with CGO_ENABLED=0, as in tifa build.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			return runCheck(cmd, root)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	return cmd
}

// runCheck runs the steps of tifa check on the application at root. Each step prints a line that
// starts with "==> ", and what its tools print goes through as they print it.
func runCheck(cmd *cobra.Command, root string) error {
	ctx, stdout, stderr := cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr()
	var (
		manifest *analyze.Manifest
		pages    bool
	)
	steps := []struct {
		name string
		run  func() error
	}{
		{"analyze", func() (err error) {
			manifest, err = analyzeApp(cmd, root)
			return err
		}},
		{"generate", func() error {
			node := bundle.NewNode(root, stderr)
			defer node.Close()
			files, err := generateApp(root, manifest, node)
			if err != nil {
				return err
			}
			_, pages = files["client.ts"]
			if err := node.Close(); err != nil {
				return fmt.Errorf("node: %w", err)
			}
			return nil
		}},
		{"go vet ./...", func() error { return goRun(ctx, root, stdout, stderr, "vet", "./...") }},
		{"go test ./...", func() error { return goRun(ctx, root, stdout, stderr, "test", "./...") }},
	}
	for _, s := range steps {
		fmt.Fprintf(stdout, "==> %s\n", s.name)
		if err := s.run(); err != nil {
			return fmt.Errorf("check failed at %s: %w", s.name, err)
		}
	}
	if !pages {
		fmt.Fprintln(stdout, "==> typescript: skipped, the app has no pages (no Node needed)")
	} else {
		fmt.Fprintln(stdout, "==> typescript (tsc --noEmit)")
		if err := typeCheck(ctx, root, stdout, stderr); err != nil {
			return fmt.Errorf("check failed at typescript (tsc --noEmit): %w", err)
		}
	}
	fmt.Fprintln(stdout, "check passed")
	return nil
}

// goRun runs the go command with args in root, with CGO_ENABLED=0 as tifa build does.
func goRun(ctx context.Context, root string, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}

// typeCheck runs tsc --noEmit of the typescript package the app installs on Node, in root, so the
// generated TypeScript in .tifa/ and the app's own files are checked together by the tsconfig.json
// of the app.
func typeCheck(ctx context.Context, root string, stdout, stderr io.Writer) error {
	tsc, err := bundle.TypeScriptCompiler(root)
	if err != nil {
		return err
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return errors.New("the type check runs tsc, which runs on Node, and node is not on PATH")
	}
	cmd := exec.CommandContext(ctx, node, tsc, "--noEmit")
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}
