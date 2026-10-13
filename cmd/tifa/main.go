// Command tifa builds and inspects tifa applications.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tifa:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tifa",
		Short:         "Build and inspect tifa applications",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newBuildCmd(), newRoutesCmd())
	return root
}
