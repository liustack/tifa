package devserver

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestDevClientShowsBrowserErrors runs testdata/client.test.mjs in Node against js/dev/client.js:
// the panel that shows the errors of the browser, its counter, the cap and the dismiss buttons.
func TestDevClientShowsBrowserErrors(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	client, err := filepath.Abs(filepath.Join("..", "..", "js", "dev", "client.js"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, node, filepath.Join("testdata", "client.test.mjs"), client).CombinedOutput()
	if err != nil {
		t.Fatalf("node client.test.mjs: %v\n%s", err, output)
	}
}
