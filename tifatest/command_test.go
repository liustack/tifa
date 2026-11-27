package tifatest_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/server"
	"github.com/liustack/tifa/tifatest"
	"github.com/spf13/cobra"
)

// greeting is an option of the app of these tests, which keeps its default in a test binary.
var greeting = tifa.String("test.greeting", "hello", "The word greet starts with")

// greetApp is an app with the command greet, which greets the names it is given, and fail, which
// returns an error. ran records the runs of OnStart and of greet.
func greetApp(ran *[]string) server.App {
	return server.App{Hooks: server.Hooks{
		OnStart: func(context.Context) error { *ran = append(*ran, "OnStart"); return nil },
		OnCommand: func(root *cobra.Command) {
			loud := false
			greet := &cobra.Command{
				Use:  "greet <name>...",
				Args: cobra.MinimumNArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					if cmd.Context().Err() != nil {
						return errors.New("the context ended before the command")
					}
					*ran = append(*ran, "greet")
					text := greeting + ", " + strings.Join(args, " and ")
					if loud {
						text = strings.ToUpper(text)
					}
					fmt.Fprintln(cmd.OutOrStdout(), text)
					fmt.Fprintln(cmd.ErrOrStderr(), "greeted", len(args))
					return nil
				},
			}
			greet.Flags().BoolVar(&loud, "loud", false, "shout")
			root.AddCommand(greet, &cobra.Command{Use: "fail", RunE: func(*cobra.Command, []string) error {
				return errors.New("no such user")
			}})
		},
	}}
}

// Command runs a command of the app as the program does, with its flags and the flags of the
// settings, and returns what it wrote and its error. OnStart does not run.
func TestCommand(t *testing.T) {
	var ran []string
	out, err := tifatest.Command(t, greetApp(&ran), "greet", "Ann", "Bo", "--loud", "--ssr-vms", "3")
	if err != nil || out != "HELLO, ANN AND BO\ngreeted 2\n" {
		t.Errorf("greet: %v, %q", err, out)
	}
	if strings.Join(ran, ",") != "greet" {
		t.Errorf("ran %q, want greet alone", ran)
	}
	// A second run makes the commands again, so --loud of the first run is gone.
	if out, err := tifatest.Command(t, greetApp(&ran), "greet", "Cy"); err != nil || out != "hello, Cy\ngreeted 1\n" {
		t.Errorf("greet again: %v, %q", err, out)
	}
	if _, err := tifatest.Command(t, greetApp(&ran), "fail"); err == nil || err.Error() != "no such user" {
		t.Errorf("fail: %v", err)
	}
	// The settings are loaded before the command, as in the program.
	if _, err := tifatest.Command(t, greetApp(&ran), "greet", "Di", "--ssr-vms", "two"); err == nil || !strings.Contains(err.Error(), "flag --ssr-vms") {
		t.Errorf("an invalid setting: %v", err)
	}
	if _, err := tifatest.Command(t, greetApp(&ran), "greet"); err == nil || !strings.Contains(err.Error(), "requires at least 1 arg") {
		t.Errorf("no name: %v", err)
	}
}

// A test that cannot run as written fails: a name that is no command of the app, an app without
// OnCommand, and a flag of an option of the app, whose value a test binary does not read.
func TestCommandFails(t *testing.T) {
	var ran []string
	for _, c := range []struct {
		app  server.App
		args []string
		want string
	}{
		{greetApp(&ran), []string{"serve"}, `"serve" names no command of the app`},
		{greetApp(&ran), []string{"version"}, `"version" names no command of the app`},
		{greetApp(&ran), nil, `"" names no command of the app`},
		{server.App{}, []string{"greet", "Ann"}, "the app has no commands"},
		{greetApp(&ran), []string{"greet", "Ann", "--test-greeting", "hi"}, "flag --test-greeting sets the option test.greeting of the app, which keeps its default in a test binary"},
	} {
		f := &fatal{TB: t}
		if out, err := tifatest.Command(f, c.app, c.args...); out != "" || err != nil || !strings.Contains(f.msg, "tifatest.Command: "+c.want) {
			t.Errorf("%q: %q, %v, message %q, want %q", c.args, out, err, f.msg, c.want)
		}
	}
	if len(ran) > 0 {
		t.Errorf("ran %q", ran)
	}
}
