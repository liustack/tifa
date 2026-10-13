// Command hostgen writes js/runtime/host.js from js/host. go generate ./js runs it in js/.
package main

import (
	"fmt"
	"os"

	"github.com/liustack/tifa/internal/bundle"
)

func main() {
	script, err := bundle.Host("host")
	if err == nil {
		err = os.WriteFile("runtime/host.js", script, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hostgen:", err)
		os.Exit(1)
	}
}
