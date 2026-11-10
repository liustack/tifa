package compare

import (
	"path/filepath"
	"slices"
)

// What a server is and how it is reached, and the server of Next.js, apart from the table of all the
// servers (specs.go): the rate of Next.js is bound to this file and not to the table (methodFiles).

// Spec is one server to measure: a Target, which framework it is, and which configurations it
// stands for. A server that is the same in two configurations is one Spec in both and runs once
// per round. To add a framework, add its Specs to the table in specs.go: the measuring, the report and the diff take
// whatever this lists. Name (of the Target) is the id of the spec.
type Spec struct {
	Target
	Framework string   // the app: "tifa", "nextjs", "start", "reactrouter"
	Label     string   // in the tables
	Configs   []string // the tables it is in
	Reference bool     // marked reference in the tables: it explains a gap and is not a contender
	// AcceptEncoding, when set, is what the client asks this spec for, instead of the global setting.
	AcceptEncoding string
	Write          Writer                               // the path of the write that changes the data of an order
	Create         func(project string) string          // the path that takes the POST which places an order in a project
	StoreWrite     func(path string) string             // the path in this app of a write of the storefront, from its path in the shop
	EditProduct    func(project, product string) string // the path that takes the PATCH which changes a product of the storefront
	Products       func(project, buildID string) string // the path whose GET lists the products of the storefront without rendering, as Tifa answers the data of the product list (buildID is the build of Tifa)
	// Metrics is the path of the metrics route of the app, which renders nothing and counts the render
	// memo (Tifa alone has one), and "" for none.
	Metrics string
	Version string // package.json, from Dir, of the framework package whose version the record keeps
}

// nodeWrite is the path of the write endpoint of the three Node apps. It is one path for all of
// them, since Next.js cannot put a route handler next to a page.
func nodeWrite(project, order string) string { return "/compare-write/" + project + "/" + order }

// nodeCreate is the path that places an order in a project in the three Node apps.
func nodeCreate(project string) string { return "/compare-write/" + project }

// nodeProducts is the path that lists the products of the storefront in the three Node apps.
func nodeProducts(project, _ string) string { return "/compare-write/" + project + "/products" }

// nodeEditProduct is the path that changes a product of the storefront in the three Node apps.
func nodeEditProduct(project, product string) string {
	return "/compare-write/" + project + "/products/" + product
}

// nextSpec is a server of the Next.js app. It is in this file, out of the table of Specs (specs.go), so
// that the rate of Next.js is bound to it and not to the servers of the other frameworks (methodFiles).
func (e Env) nextSpec(name, label string, reference bool, configs []string, flags ...string) Spec {
	nextBin := filepath.Join("node_modules", "next", "dist", "bin", "next")
	return Spec{
		Target: Target{
			Name: name, Dir: e.Next, Prog: "node",
			Args: append(slices.Clone(flags), nextBin, "start", "-H", "127.0.0.1", "-p", "%PORT%"),
			Env:  []string{"NODE_ENV=production", "NEXT_TELEMETRY_DISABLED=1"},
		},
		Framework: "nextjs", Label: label, Configs: configs, Reference: reference, Write: nodeWrite, Create: nodeCreate, StoreWrite: NextStorePath, EditProduct: nodeEditProduct, Products: nodeProducts,
		Version: filepath.Join("node_modules", "next", "package.json"),
	}
}
