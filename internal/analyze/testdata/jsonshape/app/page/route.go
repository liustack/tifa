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

// PtrText declares MarshalText on its pointer, so it is a string when held through a pointer.
type PtrText struct{ A int }

func (*PtrText) MarshalText() ([]byte, error) { return []byte("p"), nil }

type Status string

type Tags []string

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
	NumP    *json.Number   `json:"numP"`
	Raw     []byte         `json:"raw"`
	List    []int          `json:"list"`
	Grid    [2]bool        `json:"grid"`
	Empty   [0]chan int    `json:"empty"`
	None    [0]any         `json:"none"`
	When    time.Time      `json:"when"`
	Tree    *Node          `json:"tree"`
	Dur     time.Duration  `json:"dur"`
	PT      *PtrText       `json:"pt"`
	PTs     []*PtrText     `json:"pts"`
	St      Status         `json:"st"`
	ByStat  map[Status]int `json:"byStatus"`
	Tags    Tags           `json:"tags"`
	O2      int            `json:"o2,omitzero"`
	private int
}

func Load(c *tifa.Ctx) (Shape, error) { return Shape{}, nil }
