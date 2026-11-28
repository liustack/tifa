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

// serviceEnvBody goes into the folder of the service, where the program reads its .env, and sets
// the variables unitsOf gives the service besides: those of HTTPS when the app serves it itself,
// with the certificate setup uploaded or else Let's Encrypt. It needs $d, the folder of the app.
const serviceEnvBody = `cd "$d/shared" || exit 1
if [ -s "$d/https" ] && [ -f "$d/` + tlsDir + `/cert.pem" ]; then
	SERVER_TLS_CERT="$d/` + tlsDir + `/cert.pem"
	SERVER_TLS_KEY="$d/` + tlsDir + `/key.pem"
	export SERVER_TLS_CERT SERVER_TLS_KEY
elif [ -s "$d/https" ]; then
	SERVER_TLS_DOMAINS=$(cat "$d/https")
	SERVER_TLS_CACHE="$d/certs"
	export SERVER_TLS_DOMAINS SERVER_TLS_CACHE
fi
`

// runBody runs as the deploy user with the arguments of the command in $@. It runs the program of
// the current release as the service runs it.
//
// With $watch set, there is no terminal on the server, and nothing tells the program when the ssh
// connection goes away: the session process of sshd exits, and the program goes on as an orphan
// until it ends by itself. So the program runs in the background with the input of the script, and
// a watcher asks /proc every second whether the nearest sshd process above the script is still
// there. When it is gone, the watcher sends the program SIGTERM, which ends the context of a
// command of the app, and SIGKILL 10 seconds later. When the program ends, the script stops the
// watcher, which ends the sleep it waits in, and waits for it, so the session leaves no process
// behind. With a terminal, the terminal sends SIGHUP
// itself, and the program takes the place of the script as before. A server without /proc, which
// only the tests have, runs it so too.
const runBody = `d="$HOME/$app"
` + serviceEnvBody + `if [ -z "$watch" ] || [ ! -r "/proc/$$/status" ]; then exec "$d/current/$app" "$@"; fi
field() { sed -n "s/^$1:[[:space:]]*//p" "/proc/$2/status" 2>/dev/null; }
session=$(field PPid $$)
p=$session
while [ -n "$p" ] && [ "$p" -gt 1 ]; do
	case $(field Name "$p") in sshd*) session=$p; break ;; esac
	p=$(field PPid "$p")
done
exec 3<&0
"$d/current/$app" "$@" <&3 3<&- &
program=$!
exec 3<&-
(
	stop= s=
	trap 'stop=1; kill "$s" 2>/dev/null' TERM
	pause() {
		[ -z "$stop" ] || exit 0
		sleep 1 &
		s=$!
		wait "$s" || wait "$s"
		[ -z "$stop" ] || exit 0
	}
	while kill -0 "$program" 2>/dev/null; do
		if [ ! -d "/proc/$session" ]; then
			kill -TERM "$program" 2>/dev/null
			i=0
			while [ "$i" -lt 10 ] && kill -0 "$program" 2>/dev/null; do pause; i=$((i + 1)); done
			kill -KILL "$program" 2>/dev/null
			exit 0
		fi
		pause
	done
) </dev/null >/dev/null 2>&1 &
watcher=$!
wait "$program"
status=$?
kill "$watcher" 2>/dev/null
wait "$watcher"
exit "$status"
`

// commandArgs is "set --" with args, each a word of its own whatever it holds, for a script that
// runs the program with "$@".
func commandArgs(args []string) string {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = shellQuote(a)
	}
	return "set -- " + strings.Join(words, " ") + "\n"
}

// runScript is runBody for app with args. Without terminal it watches the ssh connection.
func runScript(app string, args []string, terminal bool) string {
	watch := "yes"
	if terminal {
		watch = ""
	}
	return script(commandArgs(args)+runBody, "app", app, "watch", watch)
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
	c := exec.CommandContext(ctx, sshProgram, t.sshArgs(ctx, deployUser, runScript(t.app, args, terminal), terminal)...)
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

func newDeployRunCmd(dir *string, host *[]string) *cobra.Command {
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
the input of run is the input of the program.

With several servers it runs on the first one: name another with --host.`,
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
