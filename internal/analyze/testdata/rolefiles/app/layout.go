package app

import "github.com/liustack/tifa"

func Layout(c *tifa.Ctx) (string, error) { return "root", nil }

func Load(c *tifa.Ctx) (string, error) { return "home", nil }

func POST(c *tifa.Ctx) error { return nil }

const SSR = false

const Cache = true
