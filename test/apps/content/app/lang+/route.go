// Package lang is the Layout of each language: it checks the language of the URL.
package lang

import (
	"slices"

	"github.com/liustack/tifa"

	"example.com/contenttest"
)

type Shell struct {
	Lang string `json:"lang"`
}

func Layout(c *tifa.Ctx) (Shell, error) {
	l := c.Param("lang")
	if !slices.Contains(contenttest.Blog.Languages(), l) {
		return Shell{}, tifa.ErrNotFound
	}
	return Shell{Lang: l}, nil
}
