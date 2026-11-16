// Package skills holds the agent skill that Tifa ships, so `tifa agents` can write the copy that
// belongs to the Tifa version it was built from. The files are the ones in tifa/, not a second
// copy.
package skills

import "embed"

// FS has the skill under tifa/: SKILL.md and references/.
//
//go:embed tifa
var FS embed.FS
