//go:build slowjs_cengine

package e2e

// jsonPast2000 is how the page whose data nests 2,001 levels deep ends: the C engine of slowjs
// parses JSON at most 2,000 levels deep, so the render throws and the page is a 500.
const jsonPast2000 = fails
