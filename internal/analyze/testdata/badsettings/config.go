package badsettings

import "github.com/liustack/tifa"

const key = "db.url"

var (
	DBURL   = tifa.String(key, "app.db", "not a literal")
	Name    = tifa.String("name", "x", "one part")
	Addr    = tifa.String("server.addr", "", "a key of tifa")
	A       = tifa.Int("a.b_c", 1, "")
	B       = tifa.Int("a_b.c", 1, "clashes with a.b_c")
	Twice   = tifa.Int("a.b_c", 1, "defined twice")
	Wrapped = func() string { return tifa.String("in.func", "", "") }()
)

var define = tifa.Bool

func f() bool { return tifa.Bool("in.body", false, "") }
