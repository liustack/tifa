package api

import (
	"encoding/binary"

	"github.com/liustack/tifa"
)

type Mine struct {
	ID int64 `json:"id"`
}

func GET(c *tifa.Ctx) (Mine, error) {
	var b [8]byte // filled by the caller, not from crypto/rand
	return Mine{ID: int64(binary.BigEndian.Uint64(b[:]))}, nil
}
