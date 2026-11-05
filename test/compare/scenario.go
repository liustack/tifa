package compare

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"slices"
)

// Step is one request of a scenario. Only a timed request counts in the latencies, and Page says
// which page it asks for, so that it is compared with the steady latency of that page.
type Step struct {
	Method string
	Path   string
	Body   []byte // sent as JSON
	Timed  bool
	Page   int
	Write  *Recipient // what a write sets, for checks that replay it
}

// Writer gives the path that accepts the write for an order of a project. Tifa takes the PATCH on
// the route of the order page itself, the Node apps take it on their own path (Spec.WritePath).
type Writer func(project, order string) string

// Scenario says which requests make up the n-th unit of work (n counts from 0). Exactly one of
// the steps of a unit is timed, and it is the last. Steps that are not timed, such as a write that
// changes the data of a page before the read, are sent before it and cost nothing in the numbers.
// Specs, when set, says which specs the scenario runs for, and Configs which configurations
// (all of them when nil).
type Scenario struct {
	Name    string
	Steps   func(w Writer, n int) []Step
	Specs   []string
	Configs []string
}

// The scenarios, by name.
const (
	ScenarioRead      = "read"
	ScenarioReadWrite = "read-write"
	ScenarioWriteEach = "write-each"
)

// Scenarios are all the scenarios, in the order they are measured.
var Scenarios = []Scenario{ReadScenario, ReadWriteScenario, WriteEachScenario}

// ReadScenario asks for the six pages in turn, one at a time.
var ReadScenario = Scenario{Name: ScenarioRead, Steps: func(_ Writer, n int) []Step { return []Step{read(n % len(Pages))} }}

func read(page int) Step {
	return Step{Method: http.MethodGet, Path: Pages[page].URL, Timed: true, Page: page}
}

// Recipient is a write: whom the order of a project is for, and where it goes. A write sends it back
// with a new address: the name and the phone number stay as they are, since other pages group
// orders by customer.
type Recipient struct {
	Project  string `json:"-"`
	Order    string `json:"-"`
	Customer string `json:"customer"`
	Phone    string `json:"phone"`
	Address  string `json:"address"`
}

// writable maps the pages whose data the write changes to their orders. The address of the
// recipient is shown on the page of the order only. The order lists and the stats show who an
// order is for, not where it goes, so a write cannot change them and no write comes before them.
// The names and numbers are the seed data of the shop.
var writable = map[int]Recipient{
	2: {Project: "studio", Order: "SH-1042", Customer: "林夏", Phone: "13800138000"},
	3: {Project: "weekend", Order: "WK-0201", Customer: "苏禾", Phone: "13500135004"},
}

// write is the PATCH of the recipient of the order of a page, as the form of the shop sends it,
// with an address that carries n. All addresses have the same length, so the data does not grow,
// and none repeats, so a page cannot be answered from an earlier version.
func write(w Writer, page, n int) Step {
	r := writable[page]
	r.Address = Address(n)
	body, _ := json.Marshal(r)
	return Step{Method: http.MethodPatch, Path: w(r.Project, r.Order), Body: body, Write: &r}
}

// Address is the address that the n-th write gives.
func Address(n int) string { return fmt.Sprintf("演示地址 %08d", n) }

// ReadWriteScenario reads the six pages in turn, and before the first order page after each 10
// reads it writes the address of that order, so that the page has new data and a render cache must
// miss on it. Lists and stats cannot be changed by a write (see writable), so a write waits for an
// order page.
var ReadWriteScenario = Scenario{Name: ScenarioReadWrite, Configs: []string{ConfigDefault}, Steps: func(w Writer, n int) []Step {
	page := n % len(Pages)
	if _, ok := writable[page]; !ok {
		return []Step{read(page)}
	}
	prev := n - 1 // the order page before this one: the other page of the pair, or the last cycle's
	if page == 2 {
		prev = n - 5
	}
	if floor10(prev) < floor10(n) {
		return []Step{write(w, page, n), read(page)}
	}
	return []Step{read(page)}
}}

func floor10(n int) int {
	if n < 0 {
		return -1
	}
	return n / 10
}

// WriteEachScenario is for Tifa only. It reads the two order pages in turn and writes the order
// before every read, so the render cache never hits, whatever its size. Next to a run with the cache
// off it shows what the cache costs a request that misses.
var WriteEachScenario = Scenario{Name: ScenarioWriteEach, Specs: []string{"tifa", "tifa-nocache"}, Steps: func(w Writer, n int) []Step {
	page := 2 + n%2
	return []Step{write(w, page, n), read(page)}
}}

// planned is a spec to run in a scenario.
type planned struct {
	spec     Spec
	scenario Scenario
}

// pick returns the specs the scenario runs for.
func (s Scenario) pick(specs []Spec) []Spec {
	var out []Spec
	for _, sp := range specs {
		if len(s.Specs) > 0 && !slices.Contains(s.Specs, sp.Name) {
			continue
		}
		if len(s.Configs) > 0 && !slices.ContainsFunc(sp.Configs, func(c string) bool { return slices.Contains(s.Configs, c) }) {
			continue
		}
		out = append(out, sp)
	}
	return out
}

func scenarioNames(list []Scenario) []string {
	var names []string
	for _, s := range list {
		names = append(names, s.Name)
	}
	return names
}

// ScenarioByName finds scenarios by name, or all of them for "all".
func ScenarioByName(names ...string) ([]Scenario, error) {
	var out []Scenario
	for _, name := range names {
		if name == "all" {
			return Scenarios, nil
		}
		i := slices.IndexFunc(Scenarios, func(s Scenario) bool { return s.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("no scenario named %q: have %v", name, scenarioNames(Scenarios))
		}
		out = append(out, Scenarios[i])
	}
	return out, nil
}

// Writes lists the recipients that the first reads of a scenario write, in order.
func Writes(sc Scenario, reads int) []Recipient {
	var out []Recipient
	for n := range reads {
		for _, st := range sc.Steps(nodeWrite, n) {
			if st.Write != nil {
				out = append(out, *st.Write)
			}
		}
	}
	return out
}
