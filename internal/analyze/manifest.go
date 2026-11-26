// Package analyze reads a tifa application with go/packages and go/types, checks its routing
// conventions and produces the route manifest. It never runs application code: packages are
// parsed and type checked, and dependencies are compiled to export data at most.
package analyze

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"
	"time"
)

// Manifest is the route manifest of one application. Code generation, bundling and the server
// read only this. It is plain data and marshals to JSON.
type Manifest struct {
	// Module is the module path of the application.
	Module string `json:"module"`
	// Hooks lists the hooks defined in the root package, in the order OnStart, OnShutdown, OnRequest, OnError.
	Hooks []string `json:"hooks"`
	// Session is the session type of the app, nil when nothing calls tifa.SetSession.
	Session *Session `json:"session"`
	// Settings lists the keys of the options the app defines in options.go with tifa.String and
	// the like, in the order of the file.
	Settings []string `json:"settings"`
	// I18n is the languages of the app, nil without a _locales folder.
	I18n *I18n `json:"i18n,omitempty"`
	// SiteURL is site.url of the project tifa.yaml, the scheme and host the site is served at,
	// empty when not set. With it every page gets a canonical link and the program serves
	// sitemap.xml, robots.txt and llms.txt.
	SiteURL string `json:"siteUrl,omitempty"`
	// Embedded is what the go:embed lines of the module's packages bring in. It is not part of the
	// route manifest, so it is not written out.
	Embedded *Embedded `json:"-"`
	// Content is the content collections of content.go and their entries, nil without any. The
	// build writes the entries into dist/content.json.
	Content *Content `json:"-"`
	// Warnings are problems that do not stop a build, in the order of their source positions. The
	// content has its own, see Content.Warnings.
	Warnings []Warning `json:"-"`
	// Layouts is sorted by Dir, so a Layout always comes after its parent.
	Layouts []Layout `json:"layouts"`
	// Routes is sorted by Dir.
	Routes []Route `json:"routes"`
}

// Layout is a directory under app/ whose route.go declares Layout, or that holds layout.tsx, or both.
type Layout struct {
	// ID is Dir followed by "/layout", such as "app/projects/project+/layout". The root Layout is
	// "app/layout". A route in the same directory has the ID Dir followed by "/route", whether it is a
	// page (page.tsx) or an API route (method handlers in route.go).
	ID string `json:"id"`
	// Dir is the directory relative to the application root with forward slashes, such as "app/projects/project+".
	Dir string `json:"dir"`
	// Package is the import path of the package of route.go when it declares Layout, empty when the Layout has only layout.tsx.
	Package string `json:"package"`
	// Parent is the ID of the Layout in the closest directory above Dir, empty for the outermost one.
	Parent string `json:"parent"`
	// Path is the path of Dir in ServeMux syntax, "/" for app.
	Path string `json:"path"`
	// TSX reports whether layout.tsx or layout_.tsx exists.
	TSX bool `json:"tsx"`
	// Replace reports whether the component is in layout_.tsx: the Layout shows its directory and
	// the directories below it without the Layouts above, whose Layout functions do not run for
	// them. Parent is then empty, and the Routes of the Layouts above leave out its routes.
	Replace bool `json:"replace"`
	// Load describes the Layout function, nil when the Layout has none.
	Load *Load `json:"load"`
	// Routes lists the IDs of the routes in Dir and below it, API routes included, sorted.
	Routes []string `json:"routes"`
	// Area is the folder of the _locales folder that gives the Layout its languages, empty outside
	// every area. Languages are those of the area, the default language first.
	Area      string   `json:"area,omitempty"`
	Languages []string `json:"languages,omitempty"`
}

// Route is a directory under app/ that holds page.tsx, or whose route.go declares a method handler, or both.
type Route struct {
	// ID is Dir followed by "/route", such as "app/projects/project+/route".
	ID string `json:"id"`
	// Dir is the directory relative to the application root with forward slashes.
	Dir string `json:"dir"`
	// Path is the path of Dir in ServeMux syntax: each directory below app/ is a segment, name+
	// is {name} and name++ is {name...}. "/" is the home page.
	Path string `json:"path"`
	// Package is the import path of the package of route.go, empty for a page without route.go.
	Package string `json:"package"`
	// Page reports whether page.tsx exists. The framework renders GET for a page.
	Page bool `json:"page"`
	// Load is nil when the route has no Load.
	Load *Load `json:"load"`
	// Handlers lists the method handlers route.go declares, in the order GET, POST, PUT, PATCH, DELETE.
	Handlers []Handler `json:"handlers"`
	// SSR is false when the route or any Layout in its chain sets SSR = false.
	SSR bool `json:"ssr"`
	// Memo is false when route.go sets Memo = false, and true otherwise.
	Memo bool `json:"memo"`
	// Cache is how long a shared cache may keep the page, Cache = 10 * time.Minute in route.go, zero when
	// absent.
	Cache time.Duration `json:"cache"`
	// CacheDefault is true when route.go writes Cache = true: the time is the setting cache.s_maxage.
	CacheDefault bool `json:"cacheDefault"`
	// Layouts lists the IDs of the Layouts in Dir and above it, outermost first.
	// Area is the folder of the _locales folder that gives the route its languages, empty for a
	// route outside every area, which has none and is never prefixed. Languages are those of the
	// area, the default language first.
	Area      string   `json:"area,omitempty"`
	Languages []string `json:"languages,omitempty"`
	Layouts   []string `json:"layouts"`
}

// Load describes the function that loads the data of a route (Load) or a Layout (Layout).
type Load struct {
	// Type is the Go result type T, qualified by full import paths.
	Type string `json:"type"`
	// Query lists the names of the fields of the query struct, the second parameter of Load, sorted
	// and deduplicated. A name is the json tag of the field, or its Go name. They are all the query
	// parameters the data of the Load depends on: a Layout that tifa.Use passes takes none, since it
	// cannot take a second parameter.
	Query []string `json:"query"`
	// Input is the Go type of the second parameter of Load, the query struct, qualified by full
	// import paths, and empty when Load has none.
	Input string `json:"input,omitempty"`
	// Fields are the fields of the query struct with the type of each, sorted by name. The page
	// reads the type of a parameter from them.
	Fields []QueryField `json:"fields"`
	// HTML reports whether the JSON of the result may hold a tifa.HTML value. A page the server
	// renders carries the data of such a Load with a marker in place of each one.
	HTML bool `json:"html"`
	// Result is T for code generation inside the same process. It satisfies the rules of checkTS.
	Result types.Type `json:"-"`
}

// QueryField is one field of the query struct of a Load: a query parameter.
type QueryField struct {
	// Name is the name of the parameter in the query string.
	Name string `json:"name"`
	// Type is the Go type of the field, qualified by full import paths.
	Type string `json:"type"`
	// Kind is what the values are read as: string, bool, int, int8, int16, int32, int64, uint, uint8,
	// uint16, uint32, uint64, uintptr, float32 or float64. A named type has the kind it is made of.
	Kind string `json:"kind"`
	// List is true for a slice, whose values are the parameters of that name in order.
	List bool `json:"list"`
}

// Handler describes a method handler of route.go, a function named after the HTTP method it
// answers. It takes an input after the Ctx or not, and returns a result before the error or not.
type Handler struct {
	// Method is GET, POST, PUT, PATCH or DELETE.
	Method string `json:"method"`
	// Input is the Go type of the input, qualified by full import paths, empty when the handler takes none.
	Input string `json:"input"`
	// Output is the Go type of the result, qualified by full import paths, empty when the handler returns only an error.
	Output string `json:"output"`
	// Form reports whether the input holds files, *multipart.FileHeader or []*multipart.FileHeader
	// fields. Its request body is then a form, multipart when it sends a file, and never JSON.
	Form bool `json:"form"`
	// InputType and OutputType are Input and Output for code generation inside the same process.
	// Both satisfy the rules of checkTS.
	InputType  types.Type `json:"-"`
	OutputType types.Type `json:"-"`
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
	if e.Pos.Line == 0 { // a whole file or folder, such as content/notes
		return e.Pos.Filename + ": " + e.Msg
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

// Shared reports whether the page has Cache: a time of its own, or the setting for it.
func (r Route) Shared() bool { return r.Cache > 0 || r.CacheDefault }
