// Package apitest is an application with interface routes only, built and run by test/e2e.
package apitest

import (
	"context"
	"fmt"

	"github.com/liustack/tifa"
)

func OnStart() error {
	fmt.Println("apitest: started")
	return nil
}

func OnShutdown(ctx context.Context) error {
	fmt.Println("apitest: shut down")
	return nil
}

func OnRequest(c *tifa.Ctx) error {
	c.SetHeader("X-Pattern", c.Pattern())
	return c.Next()
}

func OnError(c *tifa.Ctx, err error) tifa.Error {
	fmt.Println("apitest: unexpected error:", err)
	return tifa.Error{Status: 503, Message: "try again later"}
}
