package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// toolTimeout bounds how long tifa info waits for a program to print its version.
const toolTimeout = 5 * time.Second

const notFound = "not found"

// tifaVersion is the version of the Tifa this command was built from: the release the go.mod of an
// app requires when go tool tifa built it, and "devel" for a build in a checkout or from a replaced
// module.
func tifaVersion(info *debug.BuildInfo) string {
	if v := releaseVersion(info); v != "" {
		return v
	}
	return "devel"
}

func readBuildInfo() *debug.BuildInfo {
	info, _ := debug.ReadBuildInfo()
	return info
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of Tifa",
		Long: `version prints the version of Tifa that this command is: the release the go.mod of the app
requires when it runs as go tool tifa, and devel for a build in a checkout or from a replaced module.
tifa --version prints the same.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "tifa %s\n", tifaVersion(readBuildInfo()))
		},
	}
}

// environment is what tifa info reports, for a bug report or for a coding agent to read.
type environment struct {
	Tifa     string `json:"tifa"`
	Go       string `json:"go"`
	Node     string `json:"node"`
	Pnpm     string `json:"pnpm"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	TifaYaml bool   `json:"tifaYaml"`
}

func newInfoCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Print the versions of Tifa and its tools, and the system",
		Long: `info prints the versions of Tifa, Go, Node and pnpm, the operating system and architecture,
and whether the folder has a tifa.yaml. A program that is not on the PATH is "not found". With --json
it prints one object, with the same names in lower case: tifa, go, node, pnpm, os, arch and tifaYaml
(true or false).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			env := environment{
				Tifa: tifaVersion(readBuildInfo()),
				Go:   goVersion(),
				Node: toolVersion("node", "--version"),
				Pnpm: toolVersion("pnpm", "--version"),
				OS:   runtime.GOOS, Arch: runtime.GOARCH,
			}
			if _, err := os.Stat(filepath.Join(root, "tifa.yaml")); err == nil {
				env.TifaYaml = true
			}
			out := cmd.OutOrStdout()
			if asJSON {
				data, err := json.MarshalIndent(env, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", data)
				return nil
			}
			yaml := notFound
			if env.TifaYaml {
				yaml = "found"
			}
			fmt.Fprintf(out, "Tifa       %s\nGo         %s\nNode       %s\npnpm       %s\nSystem     %s/%s\ntifa.yaml  %s\n",
				env.Tifa, env.Go, env.Node, env.Pnpm, env.OS, env.Arch, yaml)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

// goVersion is the Go on the PATH, which builds the app, as "go1.26.1". It is not the Go that
// built this command.
func goVersion() string {
	out := toolVersion("go", "version")
	if fields := strings.Fields(out); len(fields) >= 3 && fields[0] == "go" && fields[1] == "version" {
		return fields[2]
	}
	return out
}

// toolVersion is the first line a program prints for args, or "not found" when it is not on the
// PATH or does not answer.
func toolVersion(name string, args ...string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return notFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		return notFound
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if line == "" {
		return notFound
	}
	return strings.TrimSpace(line)
}
