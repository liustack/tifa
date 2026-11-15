package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/liustack/tifa/internal/bundle"
)

func newGenCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Write the generated code into .tifa/, so editors and tsc see its types",
		Long: `gen runs the analysis of the routing conventions and writes the generated code into .tifa/,
the first two steps of tifa build, and builds nothing. tifa dev and tifa build write .tifa/ too, so
gen is for a fresh clone: the package.json that tifa new writes runs it from its prepare script,
which pnpm install and npm install run. While tifa dev or tifa build runs in the app, it keeps
.tifa/ and gen leaves it alone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			m, err := analyzeApp(cmd, root)
			if err != nil {
				return err
			}
			node := bundle.NewNode(root, cmd.ErrOrStderr())
			defer node.Close()
			_, busy, err := generateShared(root, m, node)
			if err != nil {
				return err
			}
			if busy != nil {
				fmt.Fprintln(cmd.OutOrStdout(), leftAlone(busy))
			}
			if err := node.Close(); err != nil {
				return fmt.Errorf("node: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	return cmd
}
