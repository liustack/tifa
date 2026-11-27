package main

// deploy.release_command: a command of the app that tifa deploy runs on the server with the program
// of the new release, after the upload and before the switch, while the old release serves.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// releaseCommandFailed is the result deploys.log records for a release whose deploy.release_command
// failed. Like failed, it is no place a rollback can go.
const releaseCommandFailed = "failed-release-command"

// releaseCommandExit is the exit status of releaseCommandBody when the command failed: past those of
// sh and ssh, so the status of the program, which may be any, never reads as one of them.
const releaseCommandExit = 5

// releaseCommandBody runs as the deploy user with the command in $@. It takes the lock of the deploys
// of the app, then runs the program of release $id as the service runs it, as runBody does, with
// the lock closed for the program. When the program fails it says with what status, records the
// release with $result and exits with $status: releaseCommandFailed and releaseCommandExit.
const releaseCommandBody = `d="$HOME/$app"
` + recordFunc + `exec 9>"$d/.lock"
if ! flock -n 9; then echo "tifa: another deploy of $app runs on the server" >&2; exit 1; fi
if [ ! -x "$d/releases/$id/$app" ]; then echo "tifa: the server has no release $id of $app" >&2; exit 1; fi
` + serviceEnvBody + `"$d/releases/$id/$app" "$@" 9>&-
code=$?
if [ "$code" -ne 0 ]; then
	echo "tifa: $1 exited with status $code" >&2
	record "$id" "$commit" "$result"
	exit "$status"
fi
`

// releaseCommandScript is releaseCommandBody for release id of app, deployed from commit, with args.
func releaseCommandScript(app, id, commit string, args []string) string {
	return script(commandArgs(args)+releaseCommandBody, "app", app, "id", id, "commit", commit, "action", "deploy",
		"result", releaseCommandFailed, "status", strconv.Itoa(releaseCommandExit))
}

// releaseCommand runs deploy.release_command with release id, whose program is on the server and not
// started. It runs once per deploy, before any switch, and is a step of its own next to those of
// each server (upload, activate), so that a deploy to several servers would call it once. current
// is the release that runs, empty before the first deploy: it keeps running when the command fails.
func (t deployTarget) releaseCommand(ctx context.Context, id, commit, current string, stdout, stderr io.Writer) error {
	fmt.Fprintf(stdout, "deploy.release_command: %s, with release %s\n", strings.Join(t.releaseCmd, " "), id)
	err := t.ssh(ctx, deployUser, releaseCommandScript(t.app, id, commit, t.releaseCmd), nil, stdout, stderr)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == releaseCommandExit {
		still := "no release runs yet"
		if current != "" {
			still = current + " still runs"
		}
		return &exitError{code: 1, err: fmt.Errorf("release %s failed deploy.release_command, so it was not started: %s", id, still)}
	}
	if err != nil {
		return fmt.Errorf("deploy.release_command: %w", err)
	}
	return nil
}
