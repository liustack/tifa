package page

import (
	"encoding/json"
	"time"

	"github.com/liustack/tifa"
)

// The directory is the URL.

type Base struct {
	ID    string `json:"id"`
	Kind  string
	Label string // loses to the tagged Label of Extra, at the same depth
}

type Extra struct {
	Note   string `json:"note"`
	Tagged string `json:"Label"`
}

// Level is an int written by MarshalText.
type Level int

func (Level) MarshalText() ([]byte, error) { return []byte("high"), nil }

// Code is a struct map key written by MarshalText.
type Code struct{ A, B int }

func (Code) MarshalText() ([]byte, error) { return []byte("a-b"), nil }

// PtrText declares MarshalText on its pointer, which json v2 calls on any value.
type PtrText struct{ A int }

func (*PtrText) MarshalText() ([]byte, error) { return []byte("p"), nil }

// Key is a string kind whose MarshalText json v2 calls for a map key too.
type Key string

func (Key) MarshalText() ([]byte, error) { return []byte("k"), nil }

type Status string

type Tags []string

// Octet is a named byte, so a slice of it is an array of numbers.
type Octet byte

type Node struct {
	Name string  `json:"name"`
	Kids []*Node `json:"kids"`
}

type Empty struct{}

type Shape struct {
	Base
	*Extra
	Kind    int              // shadows Base.Kind, being shallower
	Dash    int              `json:"dash-name"`
	Quoted  *int             `json:"quoted,string"`
	Count   int64            `json:"count,string"`
	Bytes4  [4]byte          `json:"bytes4"`
	Bytes0  [0]byte          `json:"bytes0"`
	Octets  []Octet          `json:"octets"`
	Opt     *bool            `json:"opt,omitempty"`
	ByNum   map[int]string   `json:"byNum"`
	ByCode  map[Code]int     `json:"byCode"`
	ByLevel map[Level]int    `json:"byLevel"`
	ByKey   map[Key]int      `json:"byKey"`
	ByPtr   map[PtrText]bool `json:"byPtr"`
	Level   Level            `json:"level"`
	Num     json.Number      `json:"num"`
	NumP    *json.Number     `json:"numP"`
	Raw     []byte           `json:"raw"`
	List    []int            `json:"list"`
	Grid    [2]bool          `json:"grid"`
	Empty   [0]chan int      `json:"empty"`
	None    [0]any           `json:"none"`
	When    time.Time        `json:"when"`
	Tree    *Node            `json:"tree"`
	PT      *PtrText         `json:"pt"`
	PTv     PtrText          `json:"ptv"`
	PTs     []*PtrText       `json:"pts"`
	St      Status           `json:"st"`
	ByStat  map[Status]int   `json:"byStatus"`
	Tags    Tags             `json:"tags"`
	O1      int              `json:"o1,omitempty"`
	O2      int              `json:"o2,omitzero"`
	Gone    Empty            `json:"gone,omitempty"`
	private int
	_       int `json:"-"`
}

func Load(c *tifa.Ctx) (Shape, error) { return Shape{}, nil }
