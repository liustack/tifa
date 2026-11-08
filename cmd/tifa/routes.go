package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

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
				data, err := json.Marshal(m, nanoseconds, jsontext.WithIndent("  "))
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(append(data, '\n'))
				return err
			}
			return printTable(cmd, m)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the route manifest as JSON")
	return cmd
}

// nanoseconds writes a time.Duration, the Cache of a route, as a number of nanoseconds, which json
// v2 has no default form for.
var nanoseconds = json.WithMarshalers(json.MarshalToFunc(func(enc *jsontext.Encoder, d time.Duration) error {
	return enc.WriteToken(jsontext.Int(int64(d)))
}))

// printTable names routes and Layouts by their directories, which is where people look for them.
func printTable(cmd *cobra.Command, m *analyze.Manifest) error {
	dirs := map[string]string{}
	for _, l := range m.Layouts {
		dirs[l.ID] = l.Dir
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ROUTE\tPATH\tLAYOUTS\tMETHODS\tSSR\tRENDERCACHE\tCACHE")
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
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%t\t%t\t%s\n", r.Dir, r.Path, dash(strings.Join(layouts, " > ")),
			dash(strings.Join(methods, " ")), r.SSR, r.Memo, cacheText(r))
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

// cacheText writes the Cache of a route: its time, "default" for Cache = true, - for none.
func cacheText(r analyze.Route) string {
	switch {
	case r.CacheDefault:
		return "default"
	case r.Cache > 0:
		return r.Cache.String()
	}
	return "-"
}
