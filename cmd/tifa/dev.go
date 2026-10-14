package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/devserver"
	"github.com/spf13/cobra"
)

// devOutput is the directory, relative to the application root, that holds the executable tifa dev
// builds. tifa dev does not watch it.
const devOutput = "bin"

func newDevCmd() *cobra.Command {
	var dir, addr string
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Run the application and rebuild it on every change",
		Long: `dev builds the application like build, runs it behind a proxy on --addr and rebuilds
on every saved change: TypeScript and CSS only need a new bundle, Go needs go build, a change
to the routes regenerates .tifa/ as well. After a rebuild the app restarts and open pages
reload. A failed build keeps the running app and shows the error in the page.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			settings, err := codegen.ProjectSettings(root)
			if err != nil {
				return err
			}
			front, err := bundle.NewDev(root, settings.Build.Target)
			if err != nil {
				return err
			}
			defer front.Close()
			exe := filepath.Join(root, devOutput, "tifa-dev")
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "tifa dev: serving http://%s\n", ln.Addr())
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return devserver.Run(ctx, ln, devserver.Options{
				Dir:    root,
				Ignore: []string{devOutput},
				App:    []string{exe, "serve", "--dist", filepath.Join(root, codegen.Dir, codegen.DistDir)},
				// The app may wait shutdown_timeout for requests, again for handlers, and once more
				// for OnShutdown.
				StopTimeout: 3*settings.Server.ShutdownTimeout + time.Second,
				Pipeline:    &devPipeline{root: root, exe: exe, front: front},
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.Flags().StringVar(&addr, "addr", config.DefaultAddr, "address the browser connects to")
	return cmd
}

// devPipeline runs the steps of tifa build for devserver, one at a time.
type devPipeline struct {
	root  string
	exe   string
	front *bundle.Dev
	m     *analyze.Manifest // the last successful analysis
	files map[string][]byte // the generated files of that analysis, as written last
}

// Analyze runs the static analysis and generates the code in memory. The fingerprint covers the
// generated files: everything a route, a Load type, a hook, an export of route.tsx or layout.tsx
// or the project configuration changes shows up there.
func (p *devPipeline) Analyze(context.Context) (string, error) {
	m, err := analyze.Analyze(p.root)
	var violations analyze.Errors
	if errors.As(err, &violations) {
		lines := make([]string, len(violations))
		for i, v := range violations {
			lines[i] = v.Error()
		}
		return "", errors.New(strings.Join(lines, "\n"))
	}
	if err != nil {
		return "", err
	}
	files, err := codegen.Generate(p.root, m, true)
	if err != nil {
		return "", err
	}
	p.m, p.files = m, files
	return fingerprint(files), nil
}

// Generate writes the files of the last analysis into .tifa.
func (p *devPipeline) Generate(context.Context) error { return codegen.Write(p.root, p.files) }

// Bundle rebuilds .tifa/dist. The route tree also depends on the exports of route.tsx and
// layout.tsx, which change without any Go change, so the TypeScript is generated again from the
// last analysis first, and written when it differs. The detail is how long Tailwind took, when
// a stylesheet uses it.
func (p *devPipeline) Bundle(context.Context) (string, error) {
	if p.m == nil {
		return "", errors.New("bundle before the first successful analysis")
	}
	files, err := codegen.Generate(p.root, p.m, true)
	if err != nil {
		return "", err
	}
	if !maps.EqualFunc(files, p.files, bytes.Equal) {
		if err := codegen.Write(p.root, files); err != nil {
			return "", err
		}
		p.files = files
	}
	settings, err := codegen.BuildSettings(p.root)
	if err != nil {
		return "", err
	}
	_, pages := files["client.ts"]
	if _, err := p.front.Build(filepath.Join(p.root, settings.Public), pages); err != nil {
		return "", err
	}
	if spent := p.front.TailwindTime(); spent > 0 {
		return "tailwind " + spent.Round(time.Millisecond).String(), nil
	}
	return "", nil
}

// GoBuild builds the executable that tifa dev runs. Its compiler messages go into the error,
// which devserver prints and shows in the page.
func (p *devPipeline) GoBuild(ctx context.Context) error {
	printed, err := goBuild(ctx, p.root, p.exe, io.Discard, io.Discard)
	if err != nil {
		return fmt.Errorf("%s\n%w", strings.TrimSpace(printed), err)
	}
	return nil
}

// fingerprint hashes file names and contents in name order. Lengths keep the boundaries apart.
func fingerprint(files map[string][]byte) string {
	h := sha256.New()
	for _, name := range slices.Sorted(maps.Keys(files)) {
		for _, part := range [][]byte{[]byte(name), files[name]} {
			_ = binary.Write(h, binary.LittleEndian, uint64(len(part)))
			h.Write(part)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
