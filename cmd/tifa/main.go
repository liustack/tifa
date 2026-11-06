// Command tifa builds and inspects tifa applications.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	err := newRootCmd().Execute()
	if err == nil {
		return
	}
	code := 1
	var exit *exitError
	if errors.As(err, &exit) {
		code, err = exit.code, exit.err
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tifa:", err)
	}
	os.Exit(code)
}

// exitError ends tifa with another status than 1, or without a message when err is nil: the
// command printed what it had to say.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit status %d", e.code)
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tifa",
		Short:         "Build and inspect tifa applications",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newNewCmd(), newBuildCmd(), newDevCmd(), newGenCmd(), newErrorsCmd(), newRoutesCmd(), newCheckCmd(), newDocsCmd())
	return root
}
