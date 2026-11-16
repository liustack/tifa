package page

import (
	"hash/fnv"
	"math"
	"math/rand"
	"time"

	"github.com/liustack/tifa"
)

type Row struct {
	Milli   int64   `json:"milli"`
	Micro   int64   `json:"micro"`
	Small   int     `json:"small"`
	Float   float64 `json:"float"`
	Tagged  int64   `json:"tagged,string"`
	Safe    int64   `json:"safe"`
	Const   int64   `json:"const"`
	Narrow  int32   `json:"narrow"`
	FromDB  int64   `json:"fromDB"`
	Bounded int64   `json:"bounded"`
	Sizes   []int64 `json:"sizes"`
	Unseen  int64   `json:"-"`
	hidden  int64
}

// Internal never reaches a page.
type Internal struct {
	At   int64
	Rand uint64
}

type dbRow struct{ ID int64 }

func fetch() dbRow { return dbRow{ID: 42} }

func internal() Internal {
	return Internal{At: time.Now().UnixNano(), Rand: rand.Uint64()}
}

func Load(c *tifa.Ctx) (Row, error) {
	at := time.Now()
	h := fnv.New64a()
	n := rand.Uint64()
	row := fetch()
	_ = internal()
	_ = n
	return Row{
		Milli:   at.UnixMilli(),
		Micro:   at.UnixMicro(),
		Small:   rand.Intn(100),
		Float:   float64(at.UnixNano()),
		Tagged:  at.UnixNano(),
		Safe:    1<<53 - 1,
		Const:   math.MaxInt32,
		Narrow:  int32(h.Sum64()),
		FromDB:  row.ID,
		Bounded: rand.Int63n(1000),
		Unseen:  at.UnixNano(),
		hidden:  at.UnixNano(),
	}, nil
}
