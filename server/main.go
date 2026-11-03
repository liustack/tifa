package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/content"
	"github.com/spf13/cobra"
)

// Main is the entry point of a generated main. It runs the command line of the application:
//
//	serve    (default) load the settings, run OnStart, serve until SIGINT or SIGTERM, run OnShutdown
//	config   print the merged runtime settings as a commented YAML file
//	version  print the module versions the program was built from
//
// Settings come from flags, environment variables, the .env file of the working directory, the
// file named by -c, the project tifa.yaml and the defaults, in that order. The settings of the
// app, which its config.go defines, were read from the same sources when its package started.
// Main exits the process with status 1 on any error, after listing every invalid setting.
func Main(app App) {
	start(app, nil)
}

// start runs the command line of app. dev, set only by DevMain, gives the server the handler
// of the requests tifa dev sends to the app, and the app the flag that ties it to tifa dev.
func start(app App, dev func(*server, fs.FS, config.SSR) http.Handler) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runSources(ctx, app, config.Process(), os.Stdout, os.Stderr, dev); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run is runSources with the sources read from args and environ, without a .env file.
func run(ctx context.Context, app App, args, environ []string, stdout, stderr io.Writer, dev func(*server, fs.FS, config.SSR) http.Handler) error {
	return runSources(ctx, app, config.Read(args, environ, ""), stdout, stderr, dev)
}

// runSources runs the command line of src.Args with the settings of src.
func runSources(ctx context.Context, app App, src *config.Sources, stdout, stderr io.Writer, dev func(*server, fs.FS, config.SSR) http.Handler) error {
	var distDir string
	lifelineFD := -1 // set by tifa dev, see watchLifeline
	serve := func(_ *cobra.Command, _ []string) (err error) {
		if lifelineFD >= 0 {
			var stop context.CancelFunc
			ctx, stop = context.WithCancel(ctx)
			defer stop()
			// The same graceful stop as SIGTERM: the app runs OnShutdown and exits.
			if err := watchLifeline(lifelineFD, stop); err != nil {
				return err
			}
		}
		cfg, _, err := loadConfig(app, src)
		if err != nil {
			return err
		}
		if dev != nil {
			cfg.Server = withoutHTTPS(cfg.Server)
		}
		s := newServer(cfg, app.Hooks)
		if dev != nil {
			s.devErrors = &devErrors{}
		}
		s.register(app.Layouts, app.Routes)
		if app.Session != nil {
			s.enableSessions(app.Session)
		}
		dist, err := frontFiles(app, distDir)
		if err != nil {
			return err
		}
		// The content collections get their entries before OnStart, which may read them. An entry
		// that fails Validate stops the program here.
		if err := content.Load(dist); err != nil {
			return err
		}
		if dist != nil {
			// openFront reads s.dev to turn on the checks of tifa dev.
			if dev != nil {
				s.dev = dev(s, dist, cfg.SSR)
			}
			if err := s.openFront(dist, cfg.SSR); err != nil {
				return err
			}
			defer func() { err = errors.Join(err, s.closeFront()) }()
		}
		return s.run(ctx)
	}
	root := &cobra.Command{
		Use:           filepath.Base(os.Args[0]),
		Short:         "Serve the application",
		Args:          cobra.NoArgs,
		RunE:          serve,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	serveCmd := &cobra.Command{Use: "serve", Short: "Serve the application (default)", Args: cobra.NoArgs, RunE: serve}
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Print every setting, its value and where the value comes from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, from, err := loadConfig(app, src)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(config.Print(cfg, from, config.AppSettings()))
			return err
		},
	}
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the versions the program was built from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), version())
			return err
		},
	}
	for _, cmd := range []*cobra.Command{root, serveCmd, configCmd, versionCmd} {
		// Flags the program does not know belong to someone else, see config.Sources.
		cmd.FParseErrWhitelist.UnknownFlags = true
	}
	for _, cmd := range []*cobra.Command{root, serveCmd, configCmd} {
		config.AddFlags(cmd.Flags())
	}
	// --dist is a development switch, not a deployment setting, so it is no setting and has no
	// environment variable.
	for _, cmd := range []*cobra.Command{root, serveCmd} {
		cmd.Flags().StringVar(&distDir, "dist", "", "serve the front end from this directory instead of the embedded one (tifa dev)")
	}
	// --lifeline-fd exists only in the main of tifa dev, so a program of tifa build has no such flag.
	if dev != nil {
		for _, cmd := range []*cobra.Command{root, serveCmd} {
			cmd.Flags().IntVar(&lifelineFD, "lifeline-fd", -1, "stop when this file descriptor reaches the end of its file, which it does when tifa dev ends (tifa dev)")
		}
	}
	root.AddCommand(serveCmd, configCmd, versionCmd)
	root.SetArgs(src.Args())
	root.SetOut(stdout)
	root.SetErr(stderr)
	return root.ExecuteContext(ctx)
}

// frontFiles returns the front end to serve: the directory named by --dist, or the embedded
// dist/ of the generated main. It is nil for an App without one, as in tests.
func frontFiles(app App, dir string) (fs.FS, error) {
	switch {
	case dir != "":
		return os.DirFS(dir), nil
	case app.Dist == nil:
		return nil, nil
	}
	return fs.Sub(app.Dist, "dist")
}

// withoutHTTPS turns off the HTTPS settings for tifa dev, whose proxy talks to the app over
// plain HTTP. Certificates of the deployment have no place on the machine of a developer.
func withoutHTTPS(c config.Server) config.Server {
	d := config.Default().Server
	c.TLSDomains, c.TLSEmail, c.TLSCache, c.TLSCA = d.TLSDomains, d.TLSEmail, d.TLSCache, d.TLSCA
	c.TLSCert, c.TLSKey, c.HTTPAddr, c.HSTS = d.TLSCert, d.TLSKey, d.HTTPAddr, d.HSTS
	return c
}

// loadConfig merges the settings of tifa from src and app.Settings. Its error lists every
// problem of the sources, of the settings of tifa and of those of the app, which were read when
// the app started.
func loadConfig(app App, src *config.Sources) (config.Config, map[string]string, error) {
	cfg, from, err := config.Load(src, app.Settings)
	return cfg, from, errors.Join(src.Err(), err, config.AppErr())
}

// version reports the application module and the tifa module from the build information.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "tifa (no build information)\n"
	}
	s := fmt.Sprintf("%s %s\n", info.Main.Path, info.Main.Version)
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" || setting.Key == "vcs.modified" {
			s += fmt.Sprintf("  %s %s\n", setting.Key, setting.Value)
		}
	}
	tifa := "(devel)"
	if info.Main.Path == "github.com/liustack/tifa" {
		tifa = info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/liustack/tifa" {
			tifa = dep.Version
			if dep.Replace != nil {
				tifa += " => " + dep.Replace.Path
			}
		}
	}
	s += fmt.Sprintf("tifa %s\n%s\n", tifa, info.GoVersion)
	return s
}
