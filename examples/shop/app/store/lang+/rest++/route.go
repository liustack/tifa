// Package rest takes every path of the store no other page has, so that its 404 shows inside the
// store's Layout, in the language of the URL, rather than as the root's 404.
package rest

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (struct{}, error) { return struct{}{}, tifa.ErrNotFound }
