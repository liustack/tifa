package page

import (
	crand "crypto/rand"
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/rand"
	randv2 "math/rand/v2"
	"time"

	"github.com/liustack/tifa"

	"example.com/bigsources/hashing"
)

type Pair struct {
	A int64  `json:"a"`
	B uint64 `json:"b"`
}

type Row struct {
	Nano      int64  `json:"nano"`
	Rand      uint64 `json:"rand"`
	Hash      uint64 `json:"hash"`
	Max       int64  `json:"max"`
	Shift     int64  `json:"shift"`
	Lit       uint64 `json:"lit"`
	Converted int64  `json:"converted"`
	Local     uint64 `json:"local"`
	Assigned  int64  `json:"assigned"`
	Bytes     uint64 `json:"bytes"`
	Func      uint64 `json:"fn"`
	V1        int64  `json:"v1"`
	Pair      Pair   `json:"pair"`
	Tagged    int64  `json:"tagged,string"`
}

func Load(c *tifa.Ctx) (Row, error) {
	at := time.Now()
	h := fnv.New64a()
	h.Write([]byte("x"))
	id := rand.Uint64()
	var buf [8]byte
	crand.Read(buf[:])
	row := Row{
		Nano:      at.UnixNano(),
		Rand:      randv2.Uint64(),
		Hash:      h.Sum64(),
		Max:       math.MaxInt64,
		Shift:     1 << 60,
		Lit:       18446744073709551615,
		Converted: int64(h.Sum64()),
		Local:     id,
		Bytes:     binary.BigEndian.Uint64(buf[:]),
		Func:      hashing.Sum64String("x"),
		V1:        rand.Int63(),
		Pair:      Pair{at.UnixNano(), randv2.Uint64()},
		Tagged:    at.UnixNano(),
	}
	row.Assigned = at.UnixNano()
	return row, nil
}
