package main

// tifa deploy run runs a command of the app on its server, with the program of the current
// release, as the service runs it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// runBody runs as the deploy user with the arguments of the command in $@. It runs the program of
// the current release in the folder of the service, where the program reads its .env, with the
// variables unitsOf gives the service besides: those of HTTPS when the app serves it itself.
const runBody = `d="$HOME/$app"
cd "$d/shared" || exit 1
if [ -s "$d/https" ]; then
	SERVER_TLS_DOMAINS=$(cat "$d/https")
	SERVER_TLS_CACHE="$d/certs"
	export SERVER_TLS_DOMAINS SERVER_TLS_CACHE
fi
exec "$d/current/$app" "$@"
`

// runScript is runBody for app with args, each a word of its own whatever it holds.
func runScript(app string, args []string) string {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = shellQuote(a)
	}
	return script("set -- "+strings.Join(words, " ")+"\n"+runBody, "app", app)
}

// deployRun runs the program of the current release of the app with args, over ssh as the deploy
// user, after the same look at the server as status. With terminal, ssh gets a terminal on the
// server. The exit status of the program comes back as an exitError with no message.
func deployRun(ctx context.Context, t deployTarget, args []string, terminal bool, stdin io.Reader, stdout, stderr io.Writer) error {
	s, err := t.state(ctx, stderr)
	if err != nil {
		return err
	}
	if s.Current == "" {
		return fmt.Errorf("%s has no release on %s yet: run go tool tifa deploy first", t.app, t.host)
	}
	c := exec.CommandContext(ctx, sshProgram, t.sshArgs(ctx, deployUser, runScript(t.app, args), terminal)...)
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
	c.WaitDelay = sshWaitDelay
	err = c.Run()
	// ssh exits with the status of the program, or with 255 when the connection fails, and then
	// it has said why.
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return &exitError{code: exit.ExitCode()}
	}
	return err
}

func newDeployRunCmd(dir, host *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <command> [arguments and flags]",
		Short: "Run a command of the app on the server, as its service runs",
		Long: `run runs the program of the current release on the server with the arguments, over ssh as the
user tifa: a command the app adds in OnCommand, or config or version. It runs in the folder of the
service, /srv/tifa/<app>/shared, so the program reads the same .env and settings as the service.
The output, the error output and the exit status of the program are those of run.

The flags of run, --dir and --host, go before the command: everything from the command on goes to
the program. With a terminal on both the input and the output, ssh gets a terminal on the server
too, so a command can ask questions and Ctrl-C reaches it. Without one, ssh runs in BatchMode and
the input of run is the input of the program.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := loadDeployTarget(*dir, *host, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			terminal := stdinIsTerminal(cmd.InOrStdin()) && stdoutIsTerminal(cmd.OutOrStdout())
			return deployRun(cmd.Context(), t, args, terminal, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	// The first argument that is no flag is the command of the app, and the flags after it are its own.
	cmd.Flags().SetInterspersed(false)
	return cmd
}
