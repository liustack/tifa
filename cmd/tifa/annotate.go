package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// envGitHubActions is "true" in every step of GitHub Actions, as its documentation says.
const envGitHubActions = "GITHUB_ACTIONS"

// annotate writes a finding of tifa as a GitHub Actions workflow command, so the run and the pull
// request mark its file and line. It writes nothing outside GitHub Actions. setup-go and
// setup-node already mark what go and tsc print, but not the findings of tifa about TypeScript
// files, and they take a warning for an error. Line and column are left out when zero. It reports
// whether it wrote, and then the caller leaves out its plain line: the matcher of setup-go reads a
// line that starts with a .go file and would mark the finding a second time, as an error.
func annotate(w io.Writer, warning bool, file string, line, col int, msg string) bool {
	if w == nil || os.Getenv(envGitHubActions) != "true" {
		return false
	}
	kind := "error"
	if warning {
		kind = "warning"
	}
	props := "file=" + escapeProperty(file)
	if line > 0 {
		props += fmt.Sprintf(",line=%d", line)
	}
	if col > 0 {
		props += fmt.Sprintf(",col=%d", col)
	}
	fmt.Fprintf(w, "::%s %s::%s\n", kind, props, escapeData(msg))
	return true
}

// escapeData escapes the message of a workflow command, as the runner unescapes it.
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// escapeProperty escapes a property value, which also ends at a comma or a colon.
func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}
