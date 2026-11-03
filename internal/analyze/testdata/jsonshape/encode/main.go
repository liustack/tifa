// Command encode prints the JSON tifa writes for sample values of the Load result of app/page, one
// JSON document per line. It writes them with c.JSON, which writes the data of a Load the same way
// (core.JSONWrite). TestLoadResultShapeMatchesEncodingJSON checks each line against the TypeScript
// shape the analyzer derives. The analyzer itself never runs this.
package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"example.com/jsonshape/app/page"
	"github.com/liustack/tifa/tifatest"
)

func main() {
	yes := true
	full := page.Shape{
		Base:    page.Base{ID: "p1", Kind: "k", Label: "base"},
		Extra:   &page.Extra{Note: "n", Tagged: "extra"},
		Kind:    3,
		Dash:    3,
		Quoted:  new(5),
		Count:   42,
		Bytes4:  [4]byte{1, 2, 3, 4},
		Octets:  []page.Octet{1, 2},
		Opt:     &yes,
		ByNum:   map[int]string{1: "one"},
		ByCode:  map[page.Code]int{{A: 1, B: 2}: 3},
		ByLevel: map[page.Level]int{1: 1},
		ByKey:   map[page.Key]int{"x": 1},
		ByPtr:   map[page.PtrText]bool{{A: 1}: true},
		Level:   9,
		Num:     "12.5",
		NumP:    new(json.Number("4")),
		Raw:     []byte("raw"),
		List:    []int{1, 2},
		Grid:    [2]bool{true, false},
		When:    time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		Tree:    &page.Node{Name: "root", Kids: []*page.Node{{Name: "leaf"}, nil}},
		PT:      &page.PtrText{A: 1},
		PTv:     page.PtrText{A: 2},
		PTs:     []*page.PtrText{{A: 2}, nil},
		St:      "on",
		ByStat:  map[page.Status]int{"on": 1},
		Tags:    page.Tags{"a"},
		O1:      4,
		O2:      5,
	}
	empty := page.Shape{ // empty slices and maps, a node without kids
		ByNum: map[int]string{}, List: []int{}, Tree: &page.Node{Kids: []*page.Node{}},
	}
	t := &tb{}
	defer t.cleanUp()
	for _, v := range []page.Shape{full, {}, empty} {
		c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
		if err := c.JSON(200, v); err != nil {
			t.Fatalf("%v", err)
		}
		fmt.Println(w.Body.String())
	}
}

// tb is the testing.TB that tifatest.NewCtx needs outside a test.
type tb struct {
	testing.TB
	cleanups []func()
}

func (*tb) Helper()                             {}
func (t *tb) Cleanup(f func())                  { t.cleanups = append(t.cleanups, f) }
func (t *tb) Errorf(format string, args ...any) { t.Fatalf(format, args...) }

func (*tb) Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func (t *tb) cleanUp() {
	for i := len(t.cleanups) - 1; i >= 0; i-- {
		t.cleanups[i]()
	}
}
