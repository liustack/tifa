// Command encode prints the real encoding/json output for sample values of the Load result of
// app/page, one JSON document per line. TestLoadResultShapeMatchesEncodingJSON checks each line
// against the TypeScript shape the analyzer derives. The analyzer itself never runs this.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"example.com/jsonshape/app/page"
)

func main() {
	yes := true
	full := page.Shape{
		Base:   page.Base{ID: "p1", Kind: "k"},
		Extra:  &page.Extra{Note: "n"},
		A:      "tagged",
		X:      7,
		Dash:   3,
		Quoted: new(5),
		Flag:   true,
		Text:   "t",
		Bytes4: [4]byte{1, 2, 3, 4},
		Count:  42,
		Opt:    &yes,
		ByNum:  map[int]string{1: "one"},
		ByCode: map[page.Code]int{{A: 1, B: 2}: 3},
		Level:  9,
		Num:    "12.5",
		NumS:   "3",
		NumP:   new(json.Number("4")),
		Raw:    []byte("raw"),
		List:   []int{1, 2},
		Grid:   [2]bool{true, false},
		When:   time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		Stamp:  page.Stamp{Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		NotM:   page.NotMarshaler{V: 1},
		Value:  page.PtrMarshal{V: 2},
		Tree:   &page.Node{Name: "root", Kids: []*page.Node{{Name: "leaf"}, nil}},
	}
	empty := page.Shape{ // nil pointers, nil slices and maps, nil embedded pointer
		ByNum: map[int]string{}, List: []int{}, Tree: &page.Node{Kids: []*page.Node{}},
	}
	for _, v := range []page.Shape{full, {}, empty} {
		b, err := json.Marshal(v) // the value itself, as the server encodes a Load result
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(string(b))
	}
}
