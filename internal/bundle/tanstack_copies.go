package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A Copy is a piece of TanStack source that tifa keeps its own version of. SHA256 is the hash of
// that piece in the tested versions (see TestedReactRouter). When a hash changes after a version
// change, read the new source and the tifa code in CompareAgain, then put the new hash here.
//
// The piece is a whole file when Start is empty. Otherwise it runs from the first line that holds
// Start to the first line after it that is exactly End, both included, so that the hash ignores the
// rest of a large file. Lines end in \n in the hash, whatever the file has.
type Copy struct {
	Package string // @tanstack/react-router, @tanstack/router-core or @tanstack/history
	File    string // inside the package, such as src/ssr/RouterClient.tsx
	Start   string
	End     string
	SHA256  string
	// What to compare again with the source, in the framework.
	CompareAgain string
}

// TanStackCopies lists the pieces of TanStack the runtime copies. The sources ship in each
// package's src folder.
var TanStackCopies = []Copy{
	{
		Package: "@tanstack/react-router", File: "src/ssr/RouterClient.tsx",
		SHA256:       "40410386752900f5493b2e32e5044cd24104745c78303f3db4d3c4d477052f39",
		CompareAgain: "takeOver in js/runtime/browser.tsx (whole file hashed)",
	},
	{
		Package: "@tanstack/router-core", File: "src/load-server.ts",
		Start: "async function projectLane(", End: "}",
		SHA256:       "f7a7668cda4eba50d288a9773c3f7cf5de1f2429550338326b24f40632ee7690",
		CompareAgain: "placed() in js/runtime/server.tsx (function projectLane hashed)",
	},
	{
		Package: "@tanstack/router-core", File: "src/router.ts",
		Start: "    // Check to see if the route needs a 404 entry", End: "      : undefined",
		SHA256:       "4f5185b6f026506454f3144e1e2632d67c2eb0c8c7b504f5ef146d97c01a2aa2",
		CompareAgain: "routesAt in js/runtime/router.tsx (the 404 check of matchRoutesInternal, from its comment to _notFoundRouteId)",
	},
	{
		Package: "@tanstack/router-core", File: "src/router.ts",
		Start: "function findGlobalNotFoundRouteId(", End: "}",
		SHA256:       "25a316b6c5ec45e581ed6712b21c519a78b7a9f3abef9d96512ce953cb635b22",
		CompareAgain: "routesAt in js/runtime/router.tsx (function findGlobalNotFoundRouteId hashed)",
	},
	{
		Package: "@tanstack/router-core", File: "src/router.ts",
		Start: "      const fullPath = `${nextPathname}${searchStr}${hashStr}`", End: "        publicHref = href",
		SHA256:       "4f4c438ece32204316f86ce119a32c0c7dbc4fdac7dfc2ad204ead8fe64cd817",
		CompareAgain: "routerCorePlugin and tifaPathOnly in js/vite/plugin.js, and pathOnly in js/runtime/prefix.ts (buildLocation from fullPath to publicHref hashed)",
	},
	// What the fast path of buildLocation leans on: which object is this.rewrite, and what the
	// functions that make or call a rewrite do. A basepath that carries pathOnly into a composed
	// rewrite would change these.
	{
		Package: "@tanstack/router-core", File: "src/router.ts",
		Start: "    const nextBasepath = this.options.basepath ?? '/'", End: "          : nextRewriteOption",
		SHA256:       "437d16feb794317a256beeea992ada5f1bb1f9c0ffd24d8fe7121b65dd69a901",
		CompareAgain: "pathOnly in js/runtime/prefix.ts and routerCorePlugin in js/vite/plugin.js (update() of router-core, from nextBasepath to this.rewrite)",
	},
	{
		Package: "@tanstack/router-core", File: "src/rewrite.ts",
		Start: "export function composeRewrites(", End: "}",
		SHA256:       "711f7495abda71c5ff42165fd4f0cf03afcdef31fa355447770ea5c1dd984bbe",
		CompareAgain: "pathOnly in js/runtime/prefix.ts and routerCorePlugin in js/vite/plugin.js (function composeRewrites hashed)",
	},
	{
		Package: "@tanstack/router-core", File: "src/rewrite.ts",
		Start: "export function rewriteBasepath(", End: "}",
		SHA256:       "b30a910dc7a4868c007fe79703bf61e06bc8408e4fd5aeb26a8c0b8ee759e5e9",
		CompareAgain: "pathOnly in js/runtime/prefix.ts and routerCorePlugin in js/vite/plugin.js (function rewriteBasepath hashed)",
	},
	{
		Package: "@tanstack/router-core", File: "src/rewrite.ts",
		Start: "export function executeRewriteOutput(", End: "}",
		SHA256:       "a95b19bfd6fd9e12a89e6dafb98ed9c1b451a0d1f7a21b9bce64e73c8e7a74bd",
		CompareAgain: "routerCorePlugin in js/vite/plugin.js (function executeRewriteOutput hashed, the fast path calls output itself)",
	},
	{
		Package: "@tanstack/router-core", File: "src/router.ts",
		Start: "  buildLocation: BuildLocationFn = (opts) => {", End: "  _commitPromise: (Promise<void> & { resolve: () => void }) | undefined",
		SHA256:       "f705fcc713c7b21b8282cbd353ab92910c971ea9996e65fc39d4fe75c15654db",
		CompareAgain: "routerCoreEdits in js/vite/plugin.js (the whole buildLocation hashed: the search middlewares of a route, the params of a link and the rewrite branch)",
	},
	{
		Package: "@tanstack/history", File: "src/index.ts",
		Start: "export function normalizeProtocolRelative(", End: "}",
		SHA256:       "8bf00980d104ed46e3ab4aae78a0ca79afdacb8d6d9fe6eed5fcf8ec7b858678",
		CompareAgain: "historyPlugin in js/vite/plugin.js (function normalizeProtocolRelative hashed)",
	},
	{
		Package: "@tanstack/react-router", File: "src/headContentUtils.tsx",
		SHA256:       "ad540a3b6a693851a71d0446450e243d4c0d16c1eff940862436c7f4220b7537",
		CompareAgain: "the head code in js/runtime/head.ts (whole file hashed)",
	},
	{
		Package: "@tanstack/history", File: "src/index.ts",
		Start: "export function parseHref(", End: "}",
		SHA256:       "7268dc8090874103cda2e2da959dcf5963234362d9a056c4a8d178baff3a65e9",
		CompareAgain: "the browser history reader in js/runtime/router.tsx (function parseHref hashed)",
	},
}

// Hash returns the SHA-256 of the piece in the source text of its file, and an error when the
// start or the end is not there.
func (c Copy) Hash(source string) (string, error) {
	text := strings.ReplaceAll(source, "\r\n", "\n")
	if c.Start != "" {
		lines := strings.Split(text, "\n")
		from, to := -1, -1
		for i, line := range lines {
			if from < 0 && strings.Contains(line, c.Start) {
				from = i
			} else if from >= 0 && line == c.End {
				to = i
				break
			}
		}
		if from < 0 || to < 0 {
			return "", fmt.Errorf("%s of %s has no piece from %q to a line %q: TanStack moved it", c.File, c.Package, c.Start, c.End)
		}
		text = strings.Join(lines[from:to+1], "\n") + "\n"
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:]), nil
}

// CheckCopies hashes each of TanStackCopies in the packages the app at root installs, found as
// CheckTanStack finds them (react-router from the app, the others from react-router), and
// returns one message for each piece whose hash differs.
func CheckCopies(root string) ([]string, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	routerDir, _, err := findPackage(root, "@tanstack/react-router")
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, c := range TanStackCopies {
		dir := routerDir
		if c.Package != "@tanstack/react-router" {
			if dir, _, err = findPackage(routerDir, c.Package); err != nil {
				return nil, err
			}
		}
		source, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(c.File)))
		if err != nil {
			return nil, err
		}
		got, err := c.Hash(string(source))
		if err != nil {
			changed = append(changed, err.Error()+". Compare again: "+c.CompareAgain)
		} else if got != c.SHA256 {
			changed = append(changed, fmt.Sprintf("%s of %s changed (sha256 %s, tifa has %s). Compare again: %s", c.File, c.Package, got, c.SHA256, c.CompareAgain))
		}
	}
	return changed, nil
}
