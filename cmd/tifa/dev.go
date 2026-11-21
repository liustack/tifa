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
	var background bool
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Run the application and rebuild it on every change",
		Long: `dev builds the application like build, runs it behind a proxy on --addr together with
Vite's dev server, and rebuilds on every saved change: Vite updates TypeScript and CSS in open
pages, which then only needs a new SSR bundle, Go needs go build, a change to the routes
regenerates .tifa/ as well. A new SSR bundle is loaded by the running app, which keeps its state.
After a Go change the app restarts and open pages reload. A failed
build keeps the running app and shows the error in the page.

With --background it starts detached from the terminal, writes everything it prints to
.tifa/dev.log, and returns once the app answers. Then dev status, dev logs and dev stop look at
it and stop it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if err := checkInstalled(root); err != nil {
				return err
			}
			if background {
				return runBackground(cmd, dir, root)
			}
			// Everything below writes into .tifa, so the lock comes first.
			lock, err := lockDev(root)
			var busy *busyError
			if errors.As(err, &busy) {
				return busyDev(dir, busy.holder)
			}
			if err != nil {
				return err
			}
			defer lock.Close()
			// A tifa dev that --background started has the log of the app for its standard error.
			if stderrIsLog(root) {
				if err := lock.SetLog(logPath(root)); err != nil {
					return err
				}
			}
			// Without --addr the default port moves on when taken. A named address does not.
			ln, err := listenDev(addr, cmd.Flags().Changed("addr"))
			if err != nil {
				return err
			}
			defer ln.Close()
			if err := lock.SetURL("http://" + ln.Addr().String()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "tifa dev: serving http://%s\n", ln.Addr())
			settings, err := codegen.ProjectSettings(root)
			if err != nil {
				return err
			}
			vitePort, err := freePort()
			if err != nil {
				return err
			}
			logger := log.New(cmd.ErrOrStderr(), "tifa dev: ", log.Ltime)
			// The app and the driver of Vite get only the files tifa dev names for them.
			if err := closeInheritedOnExec(); err != nil {
				logger.Printf("the app and Vite may inherit the files tifa dev inherited: %v", err)
			}
			ctx, stop, err := stopContext(cmd.Context())
			if err != nil {
				return err
			}
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
			exe := filepath.Join(root, devOutput, "tifa-dev"+exeSuffix)
			return devserver.Run(ctx, ln, devserver.Options{
				Dir:     root,
				Ignore:  []string{devOutput},
				App:     []string{exe, "serve", "--dist", filepath.Join(root, codegen.Dir, codegen.DistDir)},
				Vite:    net.JoinHostPort("127.0.0.1", strconv.Itoa(vitePort)),
				Outside: outside,
				// The app may wait shutdown_timeout for requests, again for handlers, and once more
				// for OnShutdown.
				StopTimeout: 3*settings.Server.ShutdownTimeout + time.Second,
				Pipeline:    &devPipeline{root: root, exe: exe, front: front, warn: cmd.ErrOrStderr()},
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
			})
		},
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.Flags().StringVar(&addr, "addr", config.DefaultAddr, "address the browser connects to")
	cmd.Flags().BoolVar(&background, "background", false, "start detached from the terminal, log to .tifa/dev.log and return once the app answers")
	cmd.AddCommand(newDevStatusCmd(&dir), newDevLogsCmd(&dir), newDevStopCmd(&dir))
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
	warn  io.Writer         // where the warnings of the checks go
	m     *analyze.Manifest // the last successful analysis
	files map[string][]byte // the generated files of that analysis, as written last
	// public is build.public of that analysis, see Public.
	public string
	// converted is the Fingerprint of the collections of the last conversion that worked.
	converted string
	// warned holds the warnings of the analysis that were printed, so that a save which changes
	// nothing about them does not print them again.
	warned map[string]bool
	// steps holds the current warnings of each step, for Warnings (tifa errors lists them).
	stepsMu sync.Mutex
	steps   map[string][]string
}

// Analyze runs the static analysis and generates the code in memory. The fingerprint covers the
// generated files: everything a route, a Load type, a hook, an export of page.tsx or layout.tsx
// or the project configuration changes shows up there.
//
// The drafts are kept, as tifa dev shows them.
func (p *devPipeline) Analyze(context.Context) (string, error) {
	var lines []string
	err := p.collect("foreign files", func(w io.Writer) (err error) {
		lines, err = scanForeign(p.root, w)
		return err
	})
	if err != nil {
		return "", err
	}
	m, err := analyze.AnalyzeWith(p.root, analyze.Options{Drafts: true})
	var violations analyze.Errors
	if errors.As(err, &violations) {
		for _, v := range violations {
			lines = append(lines, v.Error())
		}
		return "", errors.New(strings.Join(lines, "\n"))
	}
	if err != nil {
		return "", err
	}
	if len(lines) > 0 {
		return "", errors.New(strings.Join(lines, "\n"))
	}
	files, err := codegen.Generate(p.root, m, true, p.front.Exports)
	if err != nil {
		return "", err
	}
	if err := checkLangAttribute(p.root, m, p.front.LangKeys); err != nil {
		return "", err
	}
	if err := checkForeign(p.root, hasPages(files), nil, p.front.Foreign); err != nil {
		return "", err
	}
	settings, err := codegen.BuildSettings(p.root)
	if err != nil {
		return "", err
	}
	p.m, p.files, p.public = m, files, publicDir(settings.Public)
	p.printWarnings(m.Warnings)
	p.setWarnings("analysis", analysisWarnings(m))
	return fingerprint(files), nil
}

// printWarnings prints the warnings of an analysis that the one before did not have.
func (p *devPipeline) printWarnings(warnings []analyze.Warning) {
	now := make(map[string]bool, len(warnings))
	for _, w := range warnings {
		now[w.String()] = true
		if !p.warned[w.String()] && p.warn != nil {
			fmt.Fprintf(p.warn, "%s (warning)\n", w)
		}
	}
	p.warned = now
}

// analysisWarnings lists the warnings of an analysis as `tifa build` prints them.
func analysisWarnings(m *analyze.Manifest) []string {
	var lines []string
	for _, w := range m.Warnings {
		lines = append(lines, w.String()+" (warning)")
	}
	return slices.Concat(lines, contentWarnings(m.Content))
}

// contentWarnings lists the warnings of the content as `tifa build` prints them.
func contentWarnings(c *analyze.Content) []string {
	if c == nil {
		return nil
	}
	var lines []string
	for _, w := range c.Warnings {
		lines = append(lines, "tifa: content: "+w+" (warning)")
	}
	return lines
}

// stepOrder is the order of Warnings.
var stepOrder = []string{"foreign files", "analysis", "foreign code", "build plugins", "content"}

// setWarnings replaces the warnings of a step.
func (p *devPipeline) setWarnings(step string, lines []string) {
	p.stepsMu.Lock()
	defer p.stepsMu.Unlock()
	if p.steps == nil {
		p.steps = map[string][]string{}
	}
	p.steps[step] = lines
}

// collect runs a check that writes its warnings, one per line, to w. They go to p.warn as before and
// replace the warnings the step had.
func (p *devPipeline) collect(step string, run func(w io.Writer) error) error {
	var buf bytes.Buffer
	err := run(&buf)
	var lines []string
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	p.setWarnings(step, lines)
	if p.warn != nil {
		_, _ = p.warn.Write(buf.Bytes())
	}
	return err
}

// Warnings lists the warnings of the last steps that worked, see devserver.Pipeline.
func (p *devPipeline) Warnings() []string {
	p.stepsMu.Lock()
	defer p.stepsMu.Unlock()
	var all []string
	for _, step := range stepOrder {
		all = append(all, p.steps[step]...)
	}
	return all
}

// HasContent says whether the app has content collections, as the last analysis saw it.
func (p *devPipeline) HasContent() bool { return p.m != nil && p.m.Content != nil }

// ContentLoaded says that the app loaded the last content, see bundle.Dev.ContentLoaded.
func (p *devPipeline) ContentLoaded() error { return p.front.ContentLoaded() }

// HasPages says whether the app has a page.tsx, as the last analysis saw it.
func (p *devPipeline) HasPages() bool {
	_, pages := p.files["client.ts"]
	return pages
}

// Content converts the entries that changed since the last conversion into .tifa/dist and writes
// content.json, see bundle.Dev.Content. It scans content/ again for the collections of the last
// analysis, which reports the same problems as the analysis, also right after one: what the
// analysis scanned may have changed since, and a conversion that failed must not keep the scan it
// failed on. A change of the collections or their schema types converts every entry.
func (p *devPipeline) Content(ctx context.Context, all bool, changed []string) (string, error) {
	if p.m == nil {
		return "", errors.New("content before the first successful analysis")
	}
	c, err := analyze.ScanContent(p.root, p.m.Content)
	if err != nil {
		return "", err
	}
	p.setWarnings("content", contentWarnings(c))
	fp := c.Fingerprint()
	all = all || fp != p.converted
	_, pages := p.files["client.ts"]
	n, err := p.front.Content(ctx, pages, contentOf(p.m, c, p.public, ""), all, changed)
	if err != nil {
		return "", err
	}
	p.converted = fp
	if c == nil {
		return "", nil
	}
	if n == 1 {
		return "1 entry converted", nil
	}
	return fmt.Sprintf("%d entries converted", n), nil
}

// Embedded says which files the Go code embeds, as the last analysis saw them.
func (p *devPipeline) Embedded() func(string) bool { return p.m.Embedded.Has }

// Public says which folder holds the static files, as the last analysis saw it.
func (p *devPipeline) Public() string { return p.public }

// Reads says which files the last SSR bundle read.
func (p *devPipeline) Reads() func(string) bool { return p.front.Reads() }

// PollFiles makes Vite's dev server poll the files, see bundle.Dev.PollFiles.
func (p *devPipeline) PollFiles(context.Context) (bool, error) { return p.front.PollFiles() }

// Generate writes the files of the last analysis into .tifa.
func (p *devPipeline) Generate(context.Context) error { return codegen.Write(p.root, p.files) }

// Bundle rebuilds the SSR bundle in .tifa/dist, and starts Vite's dev server the first time. The
// route tree also depends on the exports of page.tsx and layout.tsx, which change without any Go
// change, so the TypeScript is generated again from the last analysis first. It is written on
// every bundle: Write leaves the files that hold the right content alone and puts back one that
// something else changed or deleted.
func (p *devPipeline) Bundle(ctx context.Context) (string, error) {
	if p.m == nil {
		return "", errors.New("bundle before the first successful analysis")
	}
	files, err := codegen.Generate(p.root, p.m, true, p.front.Exports)
	if err != nil {
		return "", err
	}
	if err := checkLangAttribute(p.root, p.m, p.front.LangKeys); err != nil {
		return "", err
	}
	settings, err := codegen.BuildSettings(p.root)
	if err != nil {
		return "", err
	}
	if err := p.collect("foreign code", func(w io.Writer) error {
		if err := checkForeign(p.root, hasPages(files), w, p.front.Foreign); err != nil {
			return err
		}
		return warnPublicImports(p.root, settings.Public, hasPages(files), w)
	}); err != nil {
		return "", err
	}
	if err := codegen.Write(p.root, files); err != nil {
		return "", err
	}
	p.files = files
	_, pages := files["client.ts"]
	if err := checkTanStack(p.root, settings, pages); err != nil {
		return "", err
	}
	if err := checkReact(p.root, pages); err != nil {
		return "", err
	}
	_, err = p.front.Build(ctx, filepath.Join(p.root, settings.Public), pages)
	if err == nil {
		p.setWarnings("build plugins", p.front.Warnings())
	}
	return "", err
}

// GoBuild builds the executable that tifa dev runs. Its compiler messages go into the error, which
// devserver prints and shows in the page.
func (p *devPipeline) GoBuild(ctx context.Context) error {
	if p.m == nil {
		return errors.New("go build before the first successful analysis")
	}
	// The generated files are checked on the disk here too: a change of Go files alone bundles nothing.
	if err := codegen.Write(p.root, p.files); err != nil {
		return err
	}
	// The program of tifa dev keeps its debug info, and needs the languages of the last analysis.
	flags, err := buildFlags(ctx, p.root, config.Build{DebugInfo: true}, languagesLDFlag(p.m))
	if err != nil {
		return err
	}
	printed, err := goBuild(ctx, p.root, p.exe, flags, io.Discard, io.Discard)
	if err != nil {
		return fmt.Errorf("%s\n%w", strings.TrimSpace(printed), err)
	}
	return nil
}

// publicDir gives build.public as tifa dev watches it: relative to the application directory with
// forward slashes. A folder outside the application directory is not watched.
func publicDir(public string) string {
	if !filepath.IsLocal(public) {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(public))
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
