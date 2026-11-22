package main

// tifa deploy env edits the .env of the app on the server: the file the app reads from the folder
// it runs in, /srv/tifa/<app>/shared/.env. It is read as configuration by the app and by nothing
// else: it is not put into the environment of the process or of systemd, so each app keeps its
// own settings. Values come in on the standard input or a hidden prompt, never as an argument, and
// no command prints one.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// bareEnvValue matches a value that needs no quotes in .env.
var bareEnvValue = regexp.MustCompile(`^[A-Za-z0-9_./:@+,=-]*$`)

// envLineName returns the name a line of .env sets, or "" for a blank line, a comment or a line
// that sets nothing.
func envLineName(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	if rest, ok := strings.CutPrefix(line, "export "); ok {
		line = strings.TrimSpace(rest)
	}
	name, _, ok := strings.Cut(line, "=")
	name = strings.TrimSpace(name)
	if !ok || !envName.MatchString(name) {
		return ""
	}
	return name
}

// envLine writes name=value the way the program reads it back: bare when that is safe, else in
// double quotes with \\, \", \n and \t.
func envLine(name, value string) string {
	if bareEnvValue.MatchString(value) && !strings.HasPrefix(value, "=") {
		return name + "=" + value
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return name + `="` + r.Replace(value) + `"`
}

// withEnv returns the file with name set: the first line of the name is replaced, later lines of
// the same name go, and every other line and comment stays as it is. A new name is appended.
func withEnv(data, name, value string) string {
	lines := splitEnv(data)
	var out []string
	done := false
	for _, l := range lines {
		if envLineName(l) == name {
			if !done {
				out = append(out, envLine(name, value))
				done = true
			}
			continue
		}
		out = append(out, l)
	}
	if !done {
		out = append(out, envLine(name, value))
	}
	return strings.Join(out, "\n") + "\n"
}

// withoutEnv returns the file without the lines that set name, and whether there were any.
func withoutEnv(data, name string) (string, bool) {
	var out []string
	found := false
	for _, l := range splitEnv(data) {
		if envLineName(l) == name {
			found = true
			continue
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return "", found
	}
	return strings.Join(out, "\n") + "\n", found
}

func splitEnv(data string) []string {
	data = strings.TrimSuffix(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	if data == "" {
		return nil
	}
	return strings.Split(data, "\n")
}

// envNames lists the names a file sets, in order, once each.
func envNames(data string) []string {
	var names []string
	seen := map[string]bool{}
	for _, l := range splitEnv(data) {
		if n := envLineName(l); n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	return names
}

// readEnvBody prints the file, or nothing when it is not there.
const readEnvBody = `f="$HOME/$app/shared/.env"
if [ -f "$f" ]; then cat "$f"; fi
`

// writeEnvBody replaces the file with its input, keeping mode 600, through a temporary file in the
// same folder so the app never reads half a file.
const writeEnvBody = `set -eu
umask 077
d="$HOME/$app/shared"
tmp=$(mktemp "$d/.env.XXXXXX")
trap 'rm -f "$tmp"' EXIT
cat > "$tmp"
chmod 600 "$tmp"
mv "$tmp" "$d/.env"
trap - EXIT
`

// restartBody restarts the app through its socket, as a deploy does, and asks it for healthPath.
// It exits with restartFailed when the check fails.
const restartBody = `set -u
trap '' HUP PIPE
d="$HOME/$app"
unit="tifa-$app"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
cd "$d" || exit 1
exec 9>"$d/.lock"
if ! flock -n 9; then echo "tifa: a deploy of $app runs on the server" >&2; exit 1; fi
` + healthCheckFunc + `systemctl --user reset-failed "$unit.socket" "$unit.service" >/dev/null 2>&1
systemctl --user start "$unit.socket" && systemctl --user restart "$unit.service" || exit 1
if check; then exit 0; fi
echo "--- last lines of the log of $app ---"
journalctl --user -u "$unit.service" -n 30 --no-pager -o cat 2>/dev/null
echo "---"
exit 3
`

const restartFailed = 3

// promptSecret reads a value on a terminal without showing it. Tests replace it.
var promptSecret = func(in io.Reader, prompt io.Writer, label string) (string, error) {
	f, ok := in.(*os.File)
	if !ok {
		return "", errors.New("no terminal to ask on")
	}
	flushInput(f)
	fmt.Fprintf(prompt, "%s (hidden): ", label)
	b, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(prompt)
	return string(b), err
}

// stdinReadable says the standard input is a pipe or a file, which a script fills, empty or not.
// A character device such as /dev/null, which coding agents give, is not. On Windows Stat reads
// the kind with GetFileType: a pipe or a disk file is readable, a character device is not. Tests
// replace it.
var stdinReadable = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode().IsRegular() || st.Mode()&os.ModeNamedPipe != 0
}

// readEnvValue takes the value from a hidden prompt on a terminal, else from a pipe or a file on
// the standard input, minus one final newline, else from the page.
func readEnvValue(ctx context.Context, in io.Reader, out io.Writer, name string, t deployTarget) (string, error) {
	ask := secretAsk{
		Title:  "Value of " + name,
		For:    t.app + " on " + t.host,
		Why:    fmt.Sprintf("Tifa sets %s in the .env of %s on the server %s, then restarts the app, which reads it as a setting.", name, t.app, t.host),
		Fields: []string{name},
	}
	if !stdinIsTerminal(in) && stdinReadable(in) {
		data, err := io.ReadAll(in)
		if err != nil {
			return "", err
		}
		v := strings.TrimSuffix(string(data), "\n")
		return strings.TrimSuffix(v, "\r"), nil
	}
	values, err := askSecrets(ctx, in, out, ask)
	if err != nil {
		return "", err
	}
	return values[0], nil
}

func (t deployTarget) readEnv(ctx context.Context, stderr io.Writer) (string, error) {
	var out bytes.Buffer
	if err := t.ssh(ctx, deployUser, script(readEnvBody, "app", t.app), nil, &out, stderr); err != nil {
		return "", err
	}
	return out.String(), nil
}

func (t deployTarget) writeEnv(ctx context.Context, content string, stderr io.Writer) error {
	return t.ssh(ctx, deployUser, script(writeEnvBody, "app", t.app), strings.NewReader(content), io.Discard, stderr)
}

// changeEnv applies next in place of old, restarts the app and checks it. When the check fails, the
// old file comes back and the app restarts again.
func changeEnv(ctx context.Context, t deployTarget, s serverState, old, next, summary string, stdout, stderr io.Writer) error {
	if old == next {
		fmt.Fprintf(stdout, "%s: nothing to change\n", summary)
		return nil
	}
	if err := t.writeEnv(ctx, next, stderr); err != nil {
		return err
	}
	if s.Current == "" {
		fmt.Fprintf(stdout, "%s on %s. The app has no release yet, so nothing was restarted.\n", summary, t.host)
		return nil
	}
	restart := func() error {
		err := t.ssh(ctx, deployUser, script(restartBody, "app", t.app, "port", fmt.Sprint(s.Port),
			"health", healthPath, "tries", fmt.Sprint(healthTries)), nil, stdout, stderr)
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == restartFailed {
			return errRestartFailed
		}
		return err
	}
	err := restart()
	if err == nil {
		fmt.Fprintf(stdout, "%s on %s, %s restarted and passed its health check\n", summary, t.host, t.app)
		return nil
	}
	if !errors.Is(err, errRestartFailed) {
		return err
	}
	if werr := t.writeEnv(ctx, old, stderr); werr != nil {
		return fmt.Errorf("%s failed its health check, and the old .env could not be put back: %w", t.app, werr)
	}
	if rerr := restart(); rerr != nil {
		return &exitError{code: 1, err: fmt.Errorf("%s failed its health check with the new .env. The old .env is back, and the app fails its check with it too", t.app)}
	}
	return &exitError{code: 1, err: fmt.Errorf("%s failed its health check with the new .env. The old .env is back and the app runs again", t.app)}
}

var errRestartFailed = errors.New("the app failed its health check")

func newDeployEnvCmd(dir, host *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Set, remove and list the names in the .env of the app on the server",
		Long: `env edits /srv/tifa/<app>/shared/.env on the server over ssh, the file the app reads from the
folder it runs in. The file is read as configuration by the app only: it is not put into the
environment of the process or of systemd. A value is never an argument and never printed. After a
change the app restarts through its socket and must pass the same health check as a deploy, or the
old file comes back.`,
	}
	load := func() (deployTarget, error) { return loadDeployTarget(*dir, *host) }
	state := func(ctx context.Context, t deployTarget, stderr io.Writer) (serverState, string, error) {
		s, err := t.state(ctx, stderr)
		if err != nil {
			return s, "", err
		}
		if s.Port == 0 {
			return s, "", fmt.Errorf("%s is not on %s yet: run go tool tifa deploy first", t.app, t.host)
		}
		old, err := t.readEnv(ctx, stderr)
		return s, old, err
	}
	checkName := func(name string) error {
		if strings.Contains(name, "=") {
			return errors.New("give the name only: the value is read from the standard input, a hidden prompt or a page in your browser, so it stays out of your shell history and the process list")
		}
		if !envName.MatchString(name) {
			return fmt.Errorf("%q is not a name .env can hold: letters, digits and underscores, not starting with a digit", name)
		}
		return nil
	}
	set := &cobra.Command{
		Use:   "set NAME",
		Short: "Set NAME in .env to a value read from the standard input, a hidden prompt or a page",
		Long: `set reads the value from a hidden prompt on a terminal, else from a pipe or a file on the
standard input, empty or not. With neither, such as /dev/null, which is what an agent gives, it
opens a page on 127.0.0.1 in the browser and prints its URL: the value is typed there and never
passes through the agent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkName(args[0]); err != nil {
				return err
			}
			t, err := load()
			if err != nil {
				return err
			}
			value, err := readEnvValue(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), args[0], t)
			if err != nil {
				return err
			}
			s, old, err := state(cmd.Context(), t, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return changeEnv(cmd.Context(), t, s, old, withEnv(old, args[0], value), "set "+args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	unset := &cobra.Command{
		Use:   "unset NAME",
		Short: "Remove NAME from .env",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkName(args[0]); err != nil {
				return err
			}
			t, err := load()
			if err != nil {
				return err
			}
			s, old, err := state(cmd.Context(), t, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			next, found := withoutEnv(old, args[0])
			if !found {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is not set in the .env of %s\n", args[0], t.app)
				return nil
			}
			return changeEnv(cmd.Context(), t, s, old, next, "removed "+args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "Print the names set in .env, never the values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := load()
			if err != nil {
				return err
			}
			_, old, err := state(cmd.Context(), t, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			names := envNames(old)
			if len(names) == 0 {
				// On stderr: a script that reads the names gets none.
				fmt.Fprintf(cmd.ErrOrStderr(), "no names are set in the .env of %s. To add one: tifa deploy env set NAME\n", t.app)
			}
			for _, n := range names {
				fmt.Fprintln(cmd.OutOrStdout(), n)
			}
			return nil
		},
	}
	cmd.AddCommand(set, unset, list)
	return cmd
}
