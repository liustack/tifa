package shop

import "github.com/liustack/tifa"

// Open lets the workbench be seen without signing in. SHOP_OPEN=1 is for the comparison with
// Next.js and TanStack Start and for load tests, which cannot sign in. Do not set it on a server.
var Open = tifa.Bool("shop.open", false, "Let the workbench be seen without signing in, for comparisons and load tests")
