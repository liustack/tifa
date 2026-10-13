package page

import (
	"encoding/json"
	"time"

	"github.com/liustack/tifa"
)

const Path = "/shape"

type Base struct {
	ID   string `json:"id"`
	Kind string
}

type Extra struct {
	Note string `json:"note"`
}

// Level is an int written by MarshalText.
type Level int

func (Level) MarshalText() ([]byte, error) { return []byte("high"), nil }

// Code is a struct map key written by MarshalText.
type Code struct{ A, B int }

func (Code) MarshalText() ([]byte, error) { return []byte("a-b"), nil }

// NotMarshaler has a MarshalJSON method of the wrong signature, so encoding/json ignores it.
type NotMarshaler struct {
	V int `json:"v"`
}

func (NotMarshaler) MarshalJSON() string { return "" }

// PtrMarshal marshals itself through a pointer receiver, which applies to addressable values only.
type PtrMarshal struct {
	V int `json:"v"`
}

func (*PtrMarshal) MarshalJSON() ([]byte, error) { return []byte(`"p"`), nil }

// Stamp gets time.Time's MarshalJSON by embedding.
type Stamp struct{ time.Time }

type Node struct {
	Name string  `json:"name"`
	Kids []*Node `json:"kids"`
}

type Shape struct {
	Base
	*Extra
	A       string `json:"X"`
	X       int
	Dash    int            `json:"dash-name"`
	Quoted  *int           `json:"quoted,string"`
	Flag    bool           `json:"flag,string"`
	Text    string         `json:"text,string"`
	Bytes4  [4]byte        `json:"bytes4"`
	Count   int64          `json:"count,string"`
	Opt     *bool          `json:"opt,omitempty"`
	ByNum   map[int]string `json:"byNum"`
	ByCode  map[Code]int   `json:"byCode"`
	Level   Level          `json:"level"`
	Num     json.Number    `json:"num"`
	Raw     []byte         `json:"raw"`
	List    []int          `json:"list"`
	Grid    [2]bool        `json:"grid"`
	Empty   [0]chan int    `json:"empty"`
	None    [0]any         `json:"none"`
	When    time.Time      `json:"when"`
	Stamp   Stamp          `json:"stamp"`
	NotM    NotMarshaler   `json:"notM"`
	Value   PtrMarshal     `json:"value"`
	Tree    *Node          `json:"tree"`
	private int
}

func Load(c *tifa.Ctx) (Shape, error) { return Shape{}, nil }
