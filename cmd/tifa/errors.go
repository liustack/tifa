package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/liustack/tifa/internal/codegen"
	"github.com/liustack/tifa/internal/deverrors"
	"github.com/spf13/cobra"
)

// errorsWait bounds how long tifa errors waits for tifa dev, which answers once the build that
// runs has ended. The first build of an app can take a minute.
const errorsWait = 3 * time.Minute

func newErrorsCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "errors",
		Short: "Print the errors tifa dev saw since the last build",
		Long: `errors asks the tifa dev that runs in the application for the errors since its last build
started: a build that failed or an app that did not start or stopped, the unexpected errors of
requests, the errors of the browser that its pages reported (hydration mismatches, uncaught
errors, unhandled rejections) and pages whose two renders differ. Run it after changing code and
after opening or reloading the pages you changed. During a build it waits for the build to end.

It exits with status 0 when there is no error, 1 when there are errors, and 2 when no tifa dev
runs in the application.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			url, err := devURL(root)
			if err != nil {
				return &exitError{code: 2, err: err}
			}
			if url == "" {
				return &exitError{code: 2, err: errors.New("tifa dev is not running in this app. Start it with go tool tifa dev")}
			}
			report, err := askDev(url)
			if err != nil {
				return &exitError{code: 2, err: err}
			}
			out := cmd.OutOrStdout()
			if asJSON {
				var indented strings.Builder
				enc := json.NewEncoder(&indented)
				enc.SetIndent("", "  ")
				if err := enc.Encode(report.Normal()); err != nil {
					return err
				}
				_, err = io.WriteString(out, indented.String())
			} else {
				_, err = io.WriteString(out, errorsText(report))
			}
			if err != nil {
				return err
			}
			if !report.Empty() {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the errors as JSON")
	return cmd
}

// devURL returns the address of the tifa dev that runs in the app at root, empty when none does:
// no one holds the lock, or its holder has no address, as tifa build or a tifa dev that does not
// listen yet. It neither creates .tifa nor keeps the lock.
func devURL(root string) (string, error) {
	f, err := os.Open(filepath.Join(root, codegen.Dir, codegen.LockFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// Closing the file drops a lock that tryLock took.
	defer f.Close()
	free, err := tryLock(f)
	if err != nil || free {
		return "", err
	}
	var holder lockHolder
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	// A holder that is still writing its record leaves one that does not parse: no address yet.
	_ = json.Unmarshal(data, &holder)
	return holder.URL, nil
}

// askDev gets the report of the tifa dev at url.
func askDev(url string) (deverrors.Report, error) {
	var report deverrors.Report
	client := &http.Client{Timeout: errorsWait}
	resp, err := client.Get(url + deverrors.Path)
	if err != nil {
		return report, fmt.Errorf("cannot reach tifa dev at %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return report, fmt.Errorf("reading the answer of tifa dev at %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return report, fmt.Errorf("tifa dev at %s answered %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return report, fmt.Errorf("tifa dev at %s answered no report: %w", url, err)
	}
	return report, nil
}

// errorsText writes each error as a heading line with its text below, indented, and a blank line
// between errors.
func errorsText(r deverrors.Report) string {
	if r.Empty() {
		return "No errors since the last build.\n"
	}
	var entries []string
	entry := func(heading string, count int, lines ...string) {
		if count > 1 {
			heading += fmt.Sprintf(" (×%d)", count)
		}
		var b strings.Builder
		b.WriteString(heading + "\n")
		for _, text := range lines {
			for line := range strings.SplitSeq(strings.TrimRight(ansiColor.ReplaceAllString(text, ""), "\n"), "\n") {
				b.WriteString(strings.TrimRight("  "+line, " ") + "\n")
			}
		}
		entries = append(entries, b.String())
	}
	if f := r.Failure; f != nil {
		entry(f.Title, 1, f.Error)
	}
	for _, e := range r.Requests {
		var lines []string
		if e.Message != http.StatusText(e.Status) {
			lines = append(lines, "Users see: "+e.Message)
		}
		entry(fmt.Sprintf("Request failed: %s %s, %d %s", e.Method, e.URL, e.Status, http.StatusText(e.Status)), e.Count, append(lines, e.Error)...)
	}
	for _, e := range r.Browser {
		entry(fmt.Sprintf("%s in the browser: %s", e.Title, e.URL), e.Count, e.Text)
	}
	for _, e := range r.Renders {
		entry(fmt.Sprintf("Renders differently each time: %s %s", e.Method, e.URL), e.Count, e.Text)
	}
	return strings.Join(entries, "\n")
}

// ansiColor matches the color codes in the messages of Vite and the Go compiler.
var ansiColor = regexp.MustCompile(`\x1b\[[0-9;]*m`)
