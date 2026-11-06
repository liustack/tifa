// Package languages tells what tifa.Languages gave when the package was initialized, and now.
package languages

import "github.com/liustack/tifa"

// atInit is read while the package initializes, as content.Define reads it.
var atInit = tifa.Languages()

// Version is set with -ldflags -X by the tests, to see that the flags of GOFLAGS reach the program
// next to the languages.
var Version = "dev"

type Out struct {
	AtInit  []string `json:"atInit"`
	Now     []string `json:"now"`
	Version string   `json:"version"`
}

func GET(c *tifa.Ctx) (Out, error) {
	return Out{AtInit: atInit, Now: tifa.Languages(), Version: Version}, nil
}
