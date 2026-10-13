package page

import "os"

const Path = "/page"

// The analyzer must not run this. The test points TIFA_TEST_INIT_MARKER at a file and checks it stays absent.
func init() {
	if path := os.Getenv("TIFA_TEST_INIT_MARKER"); path != "" {
		os.WriteFile(path, []byte("init ran"), 0o644)
	}
}
