// Package analyze reads a tifa application with go/packages and go/types, checks its routing
// conventions and produces the route manifest. It never runs application code: packages are
// parsed and type checked, and dependencies are compiled to export data at most.
package analyze

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"
)

// Manifest is the route manifest of one application. Code generation, bundling and the server
// read only this. It is plain data and marshals to JSON.
type Manifest struct {
	// Module is the module path of the application.
	Module string `json:"module"`
	// Hooks lists the hooks defined in the root package, in the order OnStart, OnShutdown, OnRequest, OnError.
	Hooks []string `json:"hooks"`
	// Layouts is sorted by ID, so a Layout always comes after its parent.
	Layouts []Layout `json:"layouts"`
	// Routes is sorted by ID.
	Routes []Route `json:"routes"`
}

// Layout is a directory under app/ that holds layout.go, layout.tsx or both.
type Layout struct {
	// ID is the directory relative to the application root with forward slashes, such as "app/projects".
	// The root Layout is "app". Route IDs and Layout IDs share one namespace.
	ID string `json:"id"`
	// Package is the import path of the package holding layout.go, empty when the Layout has only layout.tsx.
	Package string `json:"package"`
	// Parent is the ID of the closest enclosing Layout, empty for the outermost one.
	Parent string `json:"parent"`
	// Path is the common prefix of the Paths of the pages (routes with route.tsx) in scope, in ServeMux syntax, "/" when
	// they share no segment. The router path of the Layout is this prefix minus the parent's prefix,
	// and the Layout is pathless when the two are equal.
	Path string `json:"path"`
	// TSX reports whether layout.tsx exists.
	TSX bool `json:"tsx"`
	// Load is nil when the Layout has no Load.
	Load *Load `json:"load"`
	// SSR is the Layout's own SSR constant, true when absent.
	SSR bool `json:"ssr"`
	// Routes lists the IDs of all routes in scope, interface routes included, sorted.
	Routes []string `json:"routes"`
}

// Route is a directory under app/ that holds route.go.
type Route struct {
	// ID is the directory relative to the application root with forward slashes, such as "app/projects/list".
	ID string `json:"id"`
	// Path is the Path constant as written.
	Path string `json:"path"`
	// Package is the import path of the route package.
	Package string `json:"package"`
	// Page reports whether route.tsx exists. The framework renders GET for a page.
	Page bool `json:"page"`
	// Load is nil when the route has no Load.
	Load *Load `json:"load"`
	// Methods lists the HTTP methods of the OnXxx functions in the order GET, POST, PUT, PATCH, DELETE.
	Methods []string `json:"methods"`
	// SSR is false when the route or any Layout in its chain sets SSR = false.
	SSR bool `json:"ssr"`
	// Cache is the route's Cache constant, false when absent.
	Cache bool `json:"cache"`
	// Layouts lists the IDs of the enclosing Layouts, outermost first.
	Layouts []string `json:"layouts"`
}

// Load describes a Load function.
type Load struct {
	// Type is the Go result type T, qualified by full import paths.
	Type string `json:"type"`
	// Params lists the path parameters read with c.Param in the body of Load, sorted and deduplicated.
	Params []string `json:"params"`
	// Query lists the query parameters read with c.Query in the body of Load, sorted and deduplicated.
	Query []string `json:"query"`
	// Result is T for code generation inside the same process. It satisfies the rules of checkTS.
	Result types.Type `json:"-"`
}

// Error is one convention violation at a source position.
type Error struct {
	Pos token.Position // Filename is relative to the application root
	Msg string
}

func (e Error) Error() string {
	if e.Pos.Filename == "" {
		return e.Msg
	}
	if e.Pos.Column > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.Pos.Filename, e.Pos.Line, e.Pos.Column, e.Msg)
	}
	return fmt.Sprintf("%s:%d: %s", e.Pos.Filename, e.Pos.Line, e.Msg)
}

// Errors holds every violation found in one analysis, sorted by position.
type Errors []Error

func (es Errors) Error() string {
	lines := make([]string, len(es))
	for i, e := range es {
		lines[i] = e.Error()
	}
	return strings.Join(lines, "\n")
}
