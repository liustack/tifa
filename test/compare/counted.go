package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// BuildCounted builds the shop a second time, into dir, with a counter of the calls of ordersOf and
// a path /compare-counts that answers with it. The counter is added with go build's -overlay, so
// no file of the shop changes, and it is only in this build: the one that is measured is the
// plain bin/shop. It returns the path of the program.
//
// ordersOf is the filter, copy and sort every list, detail and stats query starts with, the same
// function as in the TypeScript port, which counts its calls too. The number of calls a page makes
// is the same in both when the two do the same work.
func (e Env) BuildCounted(ctx context.Context, log io.Writer, dir string) (string, error) {
	orders := filepath.Join(e.Shop, "app", "projects", "project+", "orders")
	file := filepath.Join(orders, "order.go")
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	text := string(src)
	// The counter is in order.go itself, since tifa reads the files of the app from the disk when
	// it checks the conventions and a file that only the overlay has would not be found.
	edits := [][2]string{
		{"\t\"sync\"\n", "\t\"sync\"\n\t\"sync/atomic\"\n"},
		{"func ordersOf(projectID string) []orderRecord {\n", "// OrdersOfCalls counts the calls of ordersOf, for the comparison.\nvar OrdersOfCalls atomic.Int64\n\nfunc ordersOf(projectID string) []orderRecord {\n\tOrdersOfCalls.Add(1)\n"},
	}
	for _, edit := range edits {
		if !strings.Contains(text, edit[0]) {
			return "", fmt.Errorf("%s no longer has %q: update BuildCounted", file, strings.TrimSpace(edit[0]))
		}
		text = strings.Replace(text, edit[0], edit[1], 1)
	}
	patchedOrders := filepath.Join(dir, "patched-order.go")
	if err := os.WriteFile(patchedOrders, []byte(text), 0o644); err != nil {
		return "", err
	}
	replace := map[string]string{file: patchedOrders}

	hooks := filepath.Join(e.Shop, "hooks.go")
	src, err = os.ReadFile(hooks)
	if err != nil {
		return "", err
	}
	text = string(src)
	for _, anchor := range []string{"\t\"github.com/liustack/tifa\"\n)", "func OnRequest(c *tifa.Ctx) error {\n"} {
		if !strings.Contains(text, anchor) {
			return "", fmt.Errorf("%s no longer has %q: update BuildCounted", hooks, strings.TrimSpace(anchor))
		}
	}
	text = strings.Replace(text, "\t\"github.com/liustack/tifa\"\n)", "\t\"github.com/liustack/tifa\"\n\t\"github.com/liustack/tifa/examples/shop/app/projects/project+/orders\"\n)", 1)
	text = strings.Replace(text, "func OnRequest(c *tifa.Ctx) error {\n",
		"func OnRequest(c *tifa.Ctx) error {\n\tif c.Request().URL.Path == \"/compare-counts\" {\n\t\treturn c.JSON(200, map[string]int64{\"ordersOf\": orders.OrdersOfCalls.Load()})\n\t}\n", 1)
	patchedHooks := filepath.Join(dir, "patched-hooks.go")
	if err := os.WriteFile(patchedHooks, []byte(text), 0o644); err != nil {
		return "", err
	}
	replace[hooks] = patchedHooks

	overlay, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return "", err
	}
	overlayFile := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayFile, overlay, 0o644); err != nil {
		return "", err
	}
	exe := filepath.Join(dir, "shop-counted")
	env := []string{"GOFLAGS=" + strings.TrimSpace(os.Getenv("GOFLAGS")+" -overlay="+overlayFile)}
	if err := run(ctx, log, e.Shop, env, "go", "tool", "tifa", "build", "-o", exe); err != nil {
		return "", err
	}
	return exe, nil
}
