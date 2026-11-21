// Package realpath finds the real name of a file or folder.
package realpath

// Resolve returns the absolute path of p with every link followed and every name in its long
// form, as the system itself names the file. A folder reached through a link and through its real
// name gets one result, which is what Vite and Node, which name modules by their real paths, say.
//
// It is filepath.EvalSymlinks, which on Windows stops at a junction since Go 1.23 and leaves
// short names such as RUNNER~1 as they are. pnpm links every package of node_modules with a
// junction on Windows, so a package found there must come from Resolve to be seen at its real
// place.
func Resolve(p string) (string, error) { return resolve(p) }
