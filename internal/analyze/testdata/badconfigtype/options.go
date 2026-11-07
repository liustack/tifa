package badconfigtype

import "github.com/liustack/tifa"

// The app imports nothing of the root package, which has no hooks.go.
var DBURL = tifa.String("db.url", 123, "Database file")
