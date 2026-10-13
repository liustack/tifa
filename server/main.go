package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/liustack/tifa/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Main is the entry point of a generated main. It runs the command line of the application:
//
//	serve    (default) load the settings, run OnStart, serve until SIGINT or SIGTERM, run OnShutdown
//	config   print the merged runtime settings as a commented YAML file
//	version  print the module versions the program was built from
//
// Settings come from flags, TIFA_* environment variables, the file named by --config, the project
// tifa.yaml embedded at build time and the defaults, in that order. Main exits the process with
// status 1 on any error.
func Main(app App) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, app, os.Args[1:], os.Environ(), os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, app App, args, environ []string, stdout, stderr io.Writer) error {
	var distDir string
	serve := func(cmd *cobra.Command, _ []string) (err error) {
		cfg, err := loadConfig(cmd.Flags(), app, environ)
		if err != nil {
			return err
		}
		s := New(cfg, app.Hooks)
		s.register(app.Layouts, app.Routes)
		if dist, err := frontFiles(app, distDir); err != nil {
			return err
		} else if dist != nil {
			pool, err := s.openFront(dist, cfg.SSR)
			if err != nil {
				return err
			}
			if pool != nil {
				defer func() { err = errors.Join(err, pool.Close()) }()
			}
		}
		return s.Run(ctx)
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
		Short: "Print the merged runtime settings as a deployment config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(cmd.Flags(), app, environ)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(config.Example(cfg))
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
	for _, cmd := range []*cobra.Command{root, serveCmd, configCmd} {
		config.AddFlags(cmd.Flags())
	}
	// --dist is a development switch, not a deployment setting, so it stays out of the config
	// layers and has no TIFA_ variable.
	for _, cmd := range []*cobra.Command{root, serveCmd} {
		cmd.Flags().StringVar(&distDir, "dist", "", "serve the front end from this directory instead of the embedded one (tifa dev)")
	}
	root.AddCommand(serveCmd, configCmd, versionCmd)
	root.SetArgs(args)
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

func loadConfig(fs *pflag.FlagSet, app App, environ []string) (config.Config, error) {
	return config.Load(fs, environ, app.ConfigName, app.Config)
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
