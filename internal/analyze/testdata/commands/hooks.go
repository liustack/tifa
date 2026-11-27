package commands

import (
	"github.com/spf13/cobra"
)

func OnCommand(root *cobra.Command) {
	root.AddCommand(&cobra.Command{Use: "seed", RunE: func(*cobra.Command, []string) error { return nil }})
}
