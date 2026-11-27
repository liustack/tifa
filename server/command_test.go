package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// seedApp is an app whose OnCommand adds seed, with a flag and an argument, and a group user with
// the command promote. ran records the runs of seed and of the hooks.
func seedApp(ran *[]string) App {
	return App{Hooks: Hooks{
		OnStart:    func(context.Context) error { *ran = append(*ran, "OnStart"); return nil },
		OnShutdown: func(context.Context) error { *ran = append(*ran, "OnShutdown"); return nil },
		OnCommand: func(root *cobra.Command) {
			var count int
			seed := &cobra.Command{
				Use:   "seed [kind]",
				Short: "Add sample data",
				Args:  cobra.MaximumNArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					*ran = append(*ran, "seed "+strings.Join(args, " "))
					fmt.Fprintf(cmd.OutOrStdout(), "seeded %d\n", count)
					return nil
				},
			}
			seed.Flags().IntVar(&count, "count", 10, "how many rows to add")
			user := &cobra.Command{Use: "user", Short: "Manage users"}
			user.AddCommand(&cobra.Command{
				Use:  "promote <email>",
				Args: cobra.ExactArgs(1),
				Run: func(cmd *cobra.Command, args []string) {
					*ran = append(*ran, "promote "+args[0])
				},
			})
			root.AddCommand(seed, user)
		},
	}}
}

// A command of the app runs with its own flags and arguments, and takes the flags of the settings
// as serve does. It runs no OnStart and no OnShutdown.
func TestAppCommandRuns(t *testing.T) {
	var ran []string
	app := seedApp(&ran)
	out, errOut, err := runApp(t, context.Background(), app, nil, "seed", "--count", "3", "orders", "--ssr-vms", "2", "--db-url=x.db")
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --db-url") {
		t.Fatalf("an unknown flag: %v", err)
	}
	ran = nil
	out, errOut, err = runApp(t, context.Background(), app, nil, "seed", "--count", "3", "orders", "--ssr-vms", "2", "--server-addr", ":1")
	if err != nil {
		t.Fatalf("seed: %v\n%s", err, errOut)
	}
	if out != "seeded 3\n" || errOut != "" {
		t.Errorf("stdout %q, stderr %q", out, errOut)
	}
	if !slices.Equal(ran, []string{"seed orders"}) {
		t.Errorf("ran %q, want only seed", ran)
	}
	ran = nil
	if _, _, err := runApp(t, context.Background(), app, nil, "user", "promote", "ann@example.com", "-c", writeTemp(t, "settings.yaml", "ssr:\n  vms: 3\n")); err != nil {
		t.Fatalf("user promote: %v", err)
	}
	if !slices.Equal(ran, []string{"promote ann@example.com"}) {
		t.Errorf("ran %q, want only promote", ran)
	}
}

// The settings are loaded as serve loads them, before the command runs: an invalid one stops it.
func TestAppCommandLoadsTheSettings(t *testing.T) {
	var ran []string
	app := seedApp(&ran)
	app.Settings = map[string]string{"ssr.vms": "4"}
	for _, c := range []struct {
		environ []string
		args    []string
		want    string
	}{
		{nil, []string{"seed", "--ssr-vms", "two"}, "flag --ssr-vms"},
		{[]string{"SSR_TIMEOUT=soon"}, []string{"seed"}, "env SSR_TIMEOUT"},
		{nil, []string{"seed", "-c", "missing.yaml"}, "missing.yaml"},
		{nil, []string{"user", "promote", "ann@example.com", "--server-hsts=maybe"}, "flag --server-hsts"},
	} {
		_, _, err := runApp(t, context.Background(), app, c.environ, c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q with %q: %v, want an error with %q", c.args, c.environ, err, c.want)
		}
	}
	if len(ran) > 0 {
		t.Errorf("ran %q with invalid settings", ran)
	}
}

// cmd.Context() is the context of the program, which ends on SIGINT and SIGTERM.
func TestAppCommandContextEndsWithTheProgram(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "program"))
	app := App{Hooks: Hooks{OnCommand: func(root *cobra.Command) {
		root.AddCommand(&cobra.Command{Use: "wait", RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Context().Value(key{}) != "program" {
				return errors.New("not the context of the program")
			}
			cancel()
			select {
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case <-time.After(10 * time.Second):
				return errors.New("the context did not end")
			}
		}})
	}}}
	if _, _, err := runApp(t, ctx, app, nil, "wait"); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait: %v, want context.Canceled", err)
	}
}

// The error of a command comes back once, for Main to print, with no usage text and no
// "Error:" from Cobra.
func TestAppCommandError(t *testing.T) {
	app := App{Hooks: Hooks{OnCommand: func(root *cobra.Command) {
		root.AddCommand(&cobra.Command{Use: "fail", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			return errors.New("no such user")
		}})
	}}}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"fail"}, "no such user"},
		{[]string{"fail", "extra"}, `unknown command "extra" for "`},
		{[]string{"fail", "--nope"}, "unknown flag: --nope"},
	} {
		out, errOut, err := runApp(t, context.Background(), app, nil, c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.args, err, c.want)
		}
		if out != "" || errOut != "" {
			t.Errorf("%q printed stdout %q, stderr %q, want nothing: the program prints the error once", c.args, out, errOut)
		}
	}
}

// A command of the app may not take the name or an alias of a command of the program, or the
// name of another command of the app: the program fails at start and says why, whatever it was
// asked to run.
func TestAppCommandNameClash(t *testing.T) {
	for _, c := range []struct {
		cmds []*cobra.Command
		want string
	}{
		{[]*cobra.Command{{Use: "serve"}}, `command "serve" of the app has the name of a command of the program`},
		{[]*cobra.Command{{Use: "config"}}, `"config"`},
		{[]*cobra.Command{{Use: "version [what]"}}, `"version"`},
		{[]*cobra.Command{{Use: "help"}}, `"help"`},
		{[]*cobra.Command{{Use: "completion"}}, `"completion"`},
		{[]*cobra.Command{{Use: "start", Aliases: []string{"serve"}}}, `command "start" of the app has the alias "serve", the name of a command of the program`},
		{[]*cobra.Command{{Use: "seed"}, {Use: "seed"}}, `two commands of the app are named "seed"`},
		{[]*cobra.Command{{Use: "seed"}, {Use: "fill", Aliases: []string{"seed"}}}, `two commands of the app are named "seed"`},
	} {
		ran := false
		app := App{Hooks: Hooks{
			OnStart: func(context.Context) error { ran = true; return nil },
			OnCommand: func(root *cobra.Command) {
				for _, cmd := range c.cmds {
					cmd.RunE = func(*cobra.Command, []string) error { ran = true; return nil }
				}
				root.AddCommand(c.cmds...)
			},
		}}
		for _, args := range [][]string{{"version"}, {c.cmds[0].Name()}} {
			_, _, err := runApp(t, context.Background(), app, nil, args...)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s, run %q: %v, want %q", c.cmds[len(c.cmds)-1].Use, args, err, c.want)
			}
		}
		if ran {
			t.Errorf("%s ran", c.cmds[0].Use)
		}
	}
}

// A flag of a command of the app may not be one of the flags of the settings, which every command
// of the app takes.
func TestAppCommandFlagClash(t *testing.T) {
	for _, c := range []struct {
		add  func(*cobra.Command)
		want string
	}{
		{func(cmd *cobra.Command) { cmd.Flags().Int("ssr-vms", 1, "") }, `flag --ssr-vms of command "seed" is a flag of the settings`},
		{func(cmd *cobra.Command) { cmd.Flags().String("config", "", "") }, `flag --config of command "seed"`},
		{func(cmd *cobra.Command) { cmd.Flags().BoolP("clean", "c", false, "") }, `flag -c of command "seed"`},
		{func(cmd *cobra.Command) { cmd.PersistentFlags().String("server-addr", "", "") }, `flag --server-addr of command "seed"`},
	} {
		app := App{Hooks: Hooks{OnCommand: func(root *cobra.Command) {
			seed := &cobra.Command{Use: "seed", RunE: func(*cobra.Command, []string) error { return nil }}
			c.add(seed)
			root.AddCommand(seed)
		}}}
		if _, _, err := runApp(t, context.Background(), app, nil, "seed"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v, want %q", err, c.want)
		}
	}
}

// OnCommand runs once for each run of the command line. A command made once and added again,
// such as one in a package-level variable, would keep the flags of the run before: the second
// run fails and says so.
func TestAppCommandAddedTwice(t *testing.T) {
	seed := &cobra.Command{Use: "seed", RunE: func(*cobra.Command, []string) error { return nil }}
	app := App{Hooks: Hooks{OnCommand: func(root *cobra.Command) { root.AddCommand(seed) }}}
	if _, _, err := runApp(t, context.Background(), app, nil, "seed"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	_, _, err := runApp(t, context.Background(), app, nil, "seed")
	if err == nil || !strings.Contains(err.Error(), `command "seed" was added by an earlier OnCommand`) {
		t.Fatalf("second run: %v", err)
	}
}

// The help of the program lists the commands of the app next to its own, and the help of a
// command of the app lists its flags.
func TestHelpListsAppCommands(t *testing.T) {
	var ran []string
	out, _, err := runApp(t, context.Background(), seedApp(&ran), nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"seed        Add sample data", "user        Manage users", "serve       Serve the application (default)", "version"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q:\n%s", want, out)
		}
	}
	out, _, err = runApp(t, context.Background(), seedApp(&ran), nil, "seed", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Add sample data", "--count int", "--ssr-vms"} {
		if !strings.Contains(out, want) {
			t.Errorf("help of seed lacks %q:\n%s", want, out)
		}
	}
	if len(ran) > 0 {
		t.Errorf("help ran %q", ran)
	}
}

// An app without OnCommand has the commands it always had.
func TestProgramWithoutOnCommand(t *testing.T) {
	out, _, err := runApp(t, context.Background(), App{}, nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	in := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "Available Commands:":
			in = true
		case in && line == "":
			in = false
		case in:
			names = append(names, strings.Fields(line)[0])
		}
	}
	if want := []string{"completion", "config", "help", "serve", "version"}; !slices.Equal(names, want) {
		t.Errorf("commands %q, want %q", names, want)
	}
}

func writeTemp(t *testing.T, name, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
