package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/liustack/tifa/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// programCommands are the names a command of the app may not take: the commands of the program,
// and the two that Cobra adds when the command line runs.
var programCommands = []string{"serve", "config", "version", "help", "completion"}

// addedMark is the annotation addAppCommands puts on each command of the app it prepared.
const addedMark = "tifa-command"

// addAppCommands runs OnCommand on root, which holds the commands of the program, and prepares the
// commands it added: each one that runs takes the flags of the settings, and before it runs the
// settings are loaded as serve loads them, so an invalid one stops it. Its context is the one of
// the program, which ends on SIGINT and SIGTERM, and release is called once it has ended. Nothing
// of serve runs: no OnStart, no OnShutdown, no server and no VMs. A name that is taken, or a flag
// of a command that is a flag of the settings, is an error.
func addAppCommands(root *cobra.Command, app App, src *config.Sources, release func()) error {
	// A copy: Cobra sorts the slice of its commands in place.
	own := slices.Clone(root.Commands())
	app.Hooks.OnCommand(root)
	var appCmds []*cobra.Command
	names := map[string]bool{}
	for _, cmd := range root.Commands() {
		if slices.Contains(own, cmd) {
			continue
		}
		if slices.Contains(programCommands, cmd.Name()) {
			return fmt.Errorf("command %q of the app has the name of a command of the program, which has serve, config and version, and help and completion from Cobra: give it another name", cmd.Name())
		}
		for _, alias := range cmd.Aliases {
			if slices.Contains(programCommands, alias) {
				return fmt.Errorf("command %q of the app has the alias %q, the name of a command of the program: give it another alias", cmd.Name(), alias)
			}
		}
		for _, name := range append([]string{cmd.Name()}, cmd.Aliases...) {
			if names[name] {
				return fmt.Errorf("two commands of the app are named %q, by their names or aliases: give one another name", name)
			}
			names[name] = true
		}
		appCmds = append(appCmds, cmd)
	}
	var settings pflag.FlagSet
	config.AddFlags(&settings)
	var prepare func(cmd *cobra.Command) error
	prepare = func(cmd *cobra.Command) error {
		path := strings.TrimPrefix(cmd.CommandPath(), root.Name()+" ")
		if cmd.Annotations[addedMark] != "" {
			return fmt.Errorf("command %q was added by an earlier OnCommand, and would keep the flags of that run: make each command inside OnCommand, which runs once each time the command line runs, as in each tifatest.Command", path)
		}
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}
		cmd.Annotations[addedMark] = "yes"
		if cmd.Runnable() {
			if err := settingsFlagsFree(root, cmd, &settings, path); err != nil {
				return err
			}
			config.AddFlags(cmd.Flags())
			loadFirst(cmd, app, src, release)
		}
		for _, sub := range cmd.Commands() {
			if err := prepare(sub); err != nil {
				return err
			}
		}
		return nil
	}
	for _, cmd := range appCmds {
		if err := prepare(cmd); err != nil {
			return err
		}
	}
	return nil
}

// settingsFlagsFree reports a flag of cmd, or a persistent flag of a command above it, with the name
// or the short name of a flag of the settings.
func settingsFlagsFree(root, cmd *cobra.Command, settings *pflag.FlagSet, path string) error {
	sets := []*pflag.FlagSet{cmd.Flags()}
	for c := cmd; c != nil && c != root; c = c.Parent() {
		sets = append(sets, c.PersistentFlags())
	}
	var err error
	settings.VisitAll(func(f *pflag.Flag) {
		for _, fs := range sets {
			switch {
			case err != nil:
			case fs.Lookup(f.Name) != nil:
				err = fmt.Errorf("flag --%s of command %q is a flag of the settings, which every command of the app takes: give it another name", f.Name, path)
			case f.Shorthand != "" && fs.ShorthandLookup(f.Shorthand) != nil:
				err = fmt.Errorf("flag -%s of command %q is the short flag of --%s, which every command of the app takes: give it another one", f.Shorthand, path, f.Name)
			}
		}
	})
	return err
}

// loadFirst makes cmd load the settings before its Run or RunE, and call release once its context
// has ended.
func loadFirst(cmd *cobra.Command, app App, src *config.Sources, release func()) {
	run, runE := cmd.Run, cmd.RunE
	cmd.Run = nil
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if _, _, err := loadConfig(app, src); err != nil {
			return err
		}
		if release != nil {
			stop := context.AfterFunc(c.Context(), release)
			defer stop()
		}
		if runE != nil {
			return runE(c, args)
		}
		run(c, args)
		return nil
	}
}

// appCommand reports whether cmd is a command of the app, or one below it.
func appCommand(root, cmd *cobra.Command) bool {
	for cmd != nil && cmd.Parent() != root {
		cmd = cmd.Parent()
	}
	return cmd != nil && !slices.Contains(programCommands, cmd.Name())
}

// CommandTest prepares the command of the app that args name, for tifatest.Command, as the program
// runs it: OnCommand, the flags of the settings, the settings loaded before the command, and ctx
// as its context. The settings are read from args alone, without the environment or a .env file.
// The options of the app keep their defaults in a test binary, so a flag of one is an error. out
// gets what the command writes to its output and its error output. err is a mistake of the test
// or of OnCommand, and run runs the command and returns its error.
func CommandTest(ctx context.Context, app App, args []string, out io.Writer) (run func() error, err error) {
	if app.Hooks.OnCommand == nil {
		return nil, errors.New("the app has no commands: it has no OnCommand in hooks.go, or tifa check did not run since it was added")
	}
	src := config.Read(args, nil, "")
	root, err := newProgram(ctx, app, src, nil, nil)
	if err != nil {
		return nil, err
	}
	cmd, _, err := root.Find(args)
	if err != nil {
		return nil, err
	}
	if !appCommand(root, cmd) {
		return nil, fmt.Errorf("%q names no command of the app: Command runs the commands OnCommand adds", strings.Join(args, " "))
	}
	for _, s := range config.AppSettings() {
		if src.HasFlag(s.Key) {
			return nil, fmt.Errorf("flag --%s sets the option %s of the app, which keeps its default in a test binary: set the variable of options.go in the test instead", config.FlagName(s.Key), s.Key)
		}
	}
	root.SetArgs(args)
	root.SetOut(out)
	root.SetErr(out)
	return func() error { return root.ExecuteContext(ctx) }, nil
}
