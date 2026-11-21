package main

// tifa deploy logs and tifa deploy status look at the app on the server over ssh, as the deploy
// user, so nobody has to log in to the server for it.

import (
	"context"
	"errors"
	"io"
	"os/signal"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// logsBody prints the journal of the units of the app. The units are user units of the deploy
// user, which setup makes as a regular user, not a system one. journald writes the lines of a
// regular user into a journal file of that user, which the user may read, when the journal is
// kept on disk (Storage=persistent, or auto with /var/log/journal, as Ubuntu and Debian have it).
// --user -u reads that file and matches both what the program prints (_SYSTEMD_USER_UNIT=) and
// what the systemd of the user says about the unit, such as that it started, exited or failed
// (USER_UNIT=). A match on _SYSTEMD_USER_UNIT= alone misses those lines, and without --user
// journalctl adds a hint about the journals of other users. Checked on Ubuntu 24.04, systemd 255.
// journalctl -f ends by itself once ssh is gone and its output with it.
const logsBody = `unit="tifa-$app"
if [ -n "$follow" ]; then set -- -f; fi
exec journalctl --user -u "$unit.service" -u "$unit.socket" -n "$lines" --no-pager "$@"
`

// deployLogs prints the last lines of the log of the app, and with follow the new ones until
// Ctrl-C.
func deployLogs(ctx context.Context, t deployTarget, lines int, follow bool, stdout, stderr io.Writer) error {
	if lines < 0 {
		return errors.New("-n must not be negative")
	}
	f := ""
	if follow {
		f = "yes"
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, stopSignals()...)
		defer stop()
	}
	err := t.ssh(ctx, deployUser, script(logsBody, "app", t.app, "lines", strconv.Itoa(lines), "follow", f), nil, stdout, stderr)
	if !follow {
		return err
	}
	if err != nil {
		// Ctrl-C reaches ssh as well as tifa, and ssh can end before tifa has seen the signal.
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func newDeployLogsCmd(dir, host *string) *cobra.Command {
	var lines int
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print the log of the app on the server",
		Long: `logs prints the last lines of the journal of the app on the server, what it printed and what
systemd said about it, over ssh as the user tifa. With -f it then keeps printing new lines until
Ctrl-C.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := loadDeployTarget(*dir, *host)
			if err != nil {
				return err
			}
			return deployLogs(cmd.Context(), t, lines, follow, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "print this many of the last lines")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new lines until Ctrl-C")
	return cmd
}
