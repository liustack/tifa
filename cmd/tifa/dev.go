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
	"log"
	"maps"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
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
		Long: `dev builds the application like build, runs it behind a proxy on --addr together with
Vite's dev server, and rebuilds on every saved change: Vite updates TypeScript and CSS in open
pages, which then only needs a new SSR bundle, Go needs go build, a change to the routes
regenerates .tifa/ as well. A new SSR bundle is loaded by the running app, which keeps its state.
After a Go change the app restarts and open pages reload. A failed
build keeps the running app and shows the error in the page.`,
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
			vitePort, err := freePort()
			if err != nil {
				return err
			}
			logger := log.New(cmd.ErrOrStderr(), "tifa dev: ", log.Ltime)
			ctx, stop := signal.NotifyContext(cmd.Context(), stopSignals()...)
			defer stop()
			// The driver reports changes outside the root and restarts of Vite's dev server from
			// the goroutine that reads its answers. outside never blocks there, so front.Close can
			// always wait for the driver.
			outside := &devserver.Outside{}
			front, err := bundle.NewDev(root, vitePort, &lineLogger{log: logger}, outside)
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
			return devserver.Run(ctx, ln, devserver.Options{
				Dir:     root,
				Ignore:  []string{devOutput},
				App:     []string{exe, "serve", "--dist", filepath.Join(root, codegen.Dir, codegen.DistDir)},
				Vite:    net.JoinHostPort("127.0.0.1", strconv.Itoa(vitePort)),
				Outside: outside,
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

// stopSignals are the signals that stop tifa dev: Ctrl-C, SIGTERM and SIGHUP, which a terminal sends
// to the programs on it when it closes. A tifa dev that was started with SIGHUP ignored, as nohup
// does, keeps ignoring it: asking Go for the signal would turn it back on.
func stopSignals() []os.Signal {
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if !signal.Ignored(syscall.SIGHUP) {
		signals = append(signals, syscall.SIGHUP)
	}
	return signals
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
	files, err := codegen.Generate(p.root, m, true, p.front.Exports)
	if err != nil {
		return "", err
	}
	p.m, p.files = m, files
	return fingerprint(files), nil
}

// PollFiles makes Vite's dev server poll the files, see bundle.Dev.PollFiles.
func (p *devPipeline) PollFiles(context.Context) (bool, error) { return p.front.PollFiles() }

// Generate writes the files of the last analysis into .tifa.
func (p *devPipeline) Generate(context.Context) error { return codegen.Write(p.root, p.files) }

// Bundle rebuilds the SSR bundle in .tifa/dist, and starts Vite's dev server the first time. The
// route tree also depends on the exports of route.tsx and layout.tsx, which change without any Go
// change, so the TypeScript is generated again from the last analysis first, and written when it
// differs.
func (p *devPipeline) Bundle(ctx context.Context) (string, error) {
	if p.m == nil {
		return "", errors.New("bundle before the first successful analysis")
	}
	files, err := codegen.Generate(p.root, p.m, true, p.front.Exports)
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
	if err := checkTanStack(p.root, settings, pages); err != nil {
		return "", err
	}
	if err := checkReact(p.root, pages); err != nil {
		return "", err
	}
	_, err = p.front.Build(ctx, filepath.Join(p.root, settings.Public), pages)
	return "", err
}

// GoBuild builds the executable that tifa dev runs. Its compiler messages go into the error, which
// devserver prints and shows in the page.
func (p *devPipeline) GoBuild(ctx context.Context) error {
	printed, err := goBuild(ctx, p.root, p.exe, nil, io.Discard, io.Discard)
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

// freePort returns a loopback port that is free right now, for Vite's dev server.
func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// lineLogger prints what Node writes, Vite's warnings and errors, one log line per line.
type lineLogger struct {
	log  *log.Logger
	mu   sync.Mutex
	rest []byte
}

func (l *lineLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rest = append(l.rest, p...)
	for {
		i := bytes.IndexByte(l.rest, '\n')
		if i < 0 {
			return len(p), nil
		}
		if line := strings.TrimRight(string(l.rest[:i]), "\r"); strings.TrimSpace(line) != "" {
			l.log.Print(line)
		}
		l.rest = l.rest[i+1:]
	}
}
