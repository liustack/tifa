// Package apitest is an application with interface routes only, built and run by test/e2e.
package apitest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liustack/tifa"
	"github.com/spf13/cobra"
)

func OnStart(ctx context.Context) error {
	fmt.Println("apitest: started")
	return nil
}

func OnShutdown(ctx context.Context) error {
	fmt.Println("apitest: shut down")
	return nil
}

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	c.SetHeader("X-Pattern", c.Pattern())
	return next(c)
}

func OnError(c *tifa.Ctx, err error) tifa.Error {
	fmt.Println("apitest: unexpected error:", err)
	return tifa.Error{Status: 503, Message: "try again later"}
}

// OnCommand adds three commands, which test/e2e runs: hello greets with the option hello.greeting,
// wait waits for the stop signal, and fail fails.
func OnCommand(root *cobra.Command) {
	var loud, hang bool
	hello := &cobra.Command{
		Use:   "hello <name>",
		Short: "Greet someone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := Greeting + ", " + args[0]
			if loud {
				text += "!"
			}
			fmt.Fprintln(cmd.OutOrStdout(), text)
			return nil
		},
	}
	hello.Flags().BoolVar(&loud, "loud", false, "end with an exclamation mark")
	wait := &cobra.Command{
		Use:   "wait",
		Short: "Wait for Ctrl-C",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "waiting")
			<-cmd.Context().Done()
			fmt.Fprintln(cmd.OutOrStdout(), "stopping")
			if hang {
				time.Sleep(time.Hour)
			}
			return nil
		},
	}
	wait.Flags().BoolVar(&hang, "hang", false, "keep going after the stop signal")
	fail := &cobra.Command{
		Use:   "fail",
		Short: "Fail",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return errors.New("no such note") },
	}
	root.AddCommand(hello, wait, fail)
}
