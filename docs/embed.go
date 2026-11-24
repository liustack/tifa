// Package docs carries the English docs for writing an app, so tifa agents can index the docs of
// the Tifa version it was built from in the AGENTS.md of an app. It is not for apps to import.
package docs

import "embed"

// FS has the English docs of the section folders for apps (audience app), with the index.md of
// each. A test fails when a section is missing or a Chinese file or a doc for developing Tifa is in.
//
//go:embed start/*[^N].md pages-and-data/*[^N].md guides/*[^N].md build-and-deploy/*[^N].md reference/*[^N].md
var FS embed.FS
