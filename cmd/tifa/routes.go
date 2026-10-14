package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/spf13/cobra"
)

func newRoutesCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "routes",
		Short: "Check the routing conventions and print the route table",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := analyzeApp(cmd, dir)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(m)
			}
			return printTable(cmd, m)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the route manifest as JSON")
	return cmd
}

// printTable names routes and Layouts by their directories, which is where people look for them.
func printTable(cmd *cobra.Command, m *analyze.Manifest) error {
	dirs := map[string]string{}
	for _, l := range m.Layouts {
		dirs[l.ID] = l.Dir
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ROUTE\tPATH\tLAYOUTS\tMETHODS\tSSR\tCACHE")
	for _, r := range m.Routes {
		var methods []string
		if r.Page {
			methods = append(methods, "page")
		}
		for _, h := range r.Handlers {
			methods = append(methods, h.Method)
		}
		layouts := make([]string, len(r.Layouts))
		for i, id := range r.Layouts {
			layouts[i] = dirs[id]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%t\t%t\n", r.Dir, r.Path, dash(strings.Join(layouts, " > ")),
			dash(strings.Join(methods, " ")), r.SSR, r.Cache)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "LAYOUT\tPATH\tPARENT\tSSR")
	for _, l := range m.Layouts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", l.Dir, l.Path, dash(dirs[l.Parent]), strconv.FormatBool(l.SSR))
	}
	return w.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
