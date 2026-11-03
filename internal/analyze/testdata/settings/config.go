package settings

import (
	"time"

	"github.com/liustack/tifa"
)

var (
	DBURL   = tifa.String("db.url", "app.db", "Database file")
	Hosts   = tifa.Strings("feed.hosts", nil, "Feed hosts")
	Workers = tifa.Int("jobs.workers", 4, "Background workers")
	Open    = tifa.Bool("shop.open", false, "")
)

var Idle, Limit = tifa.Duration("db.pool.idle", time.Minute, ""), tifa.Size("upload.limit", 1<<20, "")
