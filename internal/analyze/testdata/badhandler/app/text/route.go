package text

import (
	"time"

	"github.com/liustack/tifa"
)

// ID is a string in TypeScript but cannot be read back from one.
type ID int

func (id ID) MarshalText() ([]byte, error) { return []byte("id"), nil }

// Code is read from a JSON string, but TypeScript says number.
type Code int

func (c *Code) UnmarshalText(b []byte) error { return nil }

// Raw reads any JSON value its own way.
type Raw struct {
	V string `json:"v"`
}

func (r *Raw) UnmarshalJSON(b []byte) error { return nil }

// Slug and Name decode what they encode.
type Slug int

func (s Slug) MarshalText() ([]byte, error)  { return nil, nil }
func (s *Slug) UnmarshalText(b []byte) error { return nil }

type Name string

// Point is a string map key by MarshalText, Pair reads its key back.
type Point struct{ X, Y int }

func (p Point) MarshalText() ([]byte, error) { return nil, nil }

type Pair struct{ A, B string }

func (p Pair) MarshalText() ([]byte, error)  { return nil, nil }
func (p *Pair) UnmarshalText(b []byte) error { return nil }

func (n *Name) UnmarshalText(b []byte) error { return nil }

type Input struct {
	ID    ID               `json:"id"`
	Codes []Code           `json:"codes"`
	Raw   *Raw             `json:"raw"`
	ByID  map[Point]string `json:"byId"`
	Slug  Slug             `json:"slug"`
	Name  Name             `json:"name"`
	At    time.Time        `json:"at"`
	Keys  map[Pair]string  `json:"keys"`
}

func POST(c *tifa.Ctx, in Input) error { return nil }

// The result is only encoded, so MarshalText alone is fine there.
func PUT(c *tifa.Ctx) (ID, error) { return 0, nil }
