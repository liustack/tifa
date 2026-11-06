// Package languages tells what tifa.Languages gave when the package was initialized, and now.
package languages

import "github.com/liustack/tifa"

// atInit is read while the package initializes, as content.Define reads it.
var atInit = tifa.Languages()

type Out struct {
	AtInit []string `json:"atInit"`
	Now    []string `json:"now"`
}

func GET(c *tifa.Ctx) (Out, error) { return Out{AtInit: atInit, Now: tifa.Languages()}, nil }
