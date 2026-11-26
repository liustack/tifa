package main

// tifa deploy logs and tifa deploy status look at the app on the server over ssh, as the deploy
// user, so nobody has to log in to the server for it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"slices"
	"strconv"
	"strings"
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

// deployStatus prints which release runs, the releases kept, the service, the answer of the
// health check now and the memory of the program.
func deployStatus(ctx context.Context, t deployTarget, stdout, stderr io.Writer) error {
	s, err := t.probe(ctx, statusScript(t.app), stderr)
	if err != nil {
		return err
	}
	if s.Port == 0 {
		return fmt.Errorf("%s is not on %s yet: run go tool tifa deploy first", t.app, t.host)
	}
	fmt.Fprint(stdout, deployStatusText(t, s, deployNow()))
	return nil
}

// deployStatusText is what status prints about the app on the server s at the time now.
func deployStatusText(t deployTarget, s serverState, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s on %s, port %d\n", t.app, t.host, s.Port)
	line := func(label, text string) { fmt.Fprintf(&b, "  %-7s  %s\n", label, text) }
	if s.Current == "" {
		line("release", "none yet: run go tool tifa deploy")
	}
	// The current release first, then the others from the newest.
	ids := []string{}
	if s.Current != "" {
		ids = append(ids, s.Current)
	}
	for _, id := range slices.Backward(s.Releases) {
		if id != s.Current {
			ids = append(ids, id)
		}
	}
	width := 0
	for _, id := range ids {
		width = max(width, len(s.latest(id).Commit))
	}
	for i, id := range ids {
		label := ""
		switch {
		case id == s.Current:
			label = "release"
		case i == 0 || ids[i-1] == s.Current:
			label = "kept"
		}
		r := s.latest(id)
		_, passed := s.good(id)
		switch {
		case r.ID == "":
			line(label, id+"  not in deploys.log")
		case passed:
			line(label, fmt.Sprintf("%s  commit %-*s  passed its health check", id, width, r.Commit))
		default:
			line(label, fmt.Sprintf("%s  commit %-*s  failed its health check", id, width, r.Commit))
		}
	}
	service := s.Active
	if service == "" {
		service = "unknown: systemd did not say"
	}
	if s.Active == "active" && s.ActiveSince != "" {
		service = "active since " + s.ActiveSince
		if since, err := time.Parse("Mon 2006-01-02 15:04:05 MST", s.ActiveSince); err == nil {
			service = fmt.Sprintf("active since %s, %s ago", since.Format("2006-01-02 15:04:05 MST"), ago(now.Sub(since)))
		}
	}
	line("service", service)
	line("health", healthLine(s.Health))
	if s.RSS > 0 {
		line("memory", fmt.Sprintf("%.1f MB", float64(s.RSS)*1024/1e6))
	}
	return b.String()
}

// ago writes a time that passed in minutes, or in seconds below one.
func ago(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return strings.TrimSuffix(d.Round(time.Minute).String(), "0s")
}

func newDeployStatusCmd(dir, host *string) *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print which release of the app runs on the server and how it is",
		Long: `status prints, over ssh as the user tifa, the release that runs and its commit, the earlier
releases kept and whether each passed its health check, since when the service runs, what
GET /healthz answers now, and the memory of the program. It asks for /healthz only while the
service runs, so it starts nothing.

With --all it needs no app checkout. It looks once at every server in ~/.tifa/config.yaml and
prints, for each, its system, CPUs, memory, disk, uptime and load, and each app on it: the release
and its commit, whether it passed its health check, the service and how often systemd restarted
it, what GET /healthz answers now and its memory. --json prints the same as one JSON document.
It exits with an error when a server cannot be reached or an app is not active with a passing
health check. Only the servers this machine has set up or deployed to, the ones in that file,
are covered.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all {
				if cmd.Flag("host").Changed || cmd.Flag("dir").Changed {
					return errors.New("--all looks at every server in " + shownServersPath() + " and takes no --host or --dir: leave them out")
				}
				return deployStatusAll(cmd.Context(), asJSON, cmd.OutOrStdout())
			}
			if asJSON {
				return errors.New("--json goes with --all")
			}
			t, err := loadDeployTarget(*dir, *host, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return deployStatus(cmd.Context(), t, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "look at every server in ~/.tifa/config.yaml once, with no app checkout")
	cmd.Flags().BoolVar(&asJSON, "json", false, "with --all, print one JSON document")
	return cmd
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
			t, err := loadDeployTarget(*dir, *host, cmd.ErrOrStderr())
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

// healthLine says what GET healthPath answered, judged the way a deploy judges it: a status below
// 500 passes, and an app without the route passes with its 404.
func healthLine(code string) string {
	switch {
	case code == "":
		return "not asked, the service does not run"
	case code == "000":
		return "GET " + healthPath + " got no answer within 5 seconds"
	case code == "404":
		return "GET " + healthPath + " answered 404, which passes: the app has no " + healthPath + " route"
	case code >= "500":
		return "GET " + healthPath + " answered " + code + ", which fails"
	}
	return "GET " + healthPath + " answered " + code
}
