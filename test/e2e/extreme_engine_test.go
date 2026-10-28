//go:build !slowjs_cengine

package e2e

// jsonPast2000 is how the page whose data nests 2,001 levels deep ends: the engine of slowjs that
// runs on Go parses JSON of any depth that fits in its stack.
const jsonPast2000 = renders
