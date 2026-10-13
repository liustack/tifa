package bundle

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
)

// SSROptions configures the SSR bundle. Relative paths are relative to Root.
type SSROptions struct {
	Root     string // project root, the directory holding node_modules
	Entry    string // SSR entry, such as the generated server.tsx
	Outfile  string // the single output file
	Tsconfig string // empty lets esbuild find the nearest tsconfig.json
	Write    bool   // false keeps the output in memory only
	// Profile keeps the code guarded by __SSR_PROFILE__. Production builds
	// leave it false so minification removes the timing code.
	Profile bool
	// Plugins run before the built-in SSR rules, for example to resolve
	// generated framework modules.
	Plugins []api.Plugin
	// Define adds global constants, such as the browser assets a page loads.
	Define map[string]string
}

// nodeBuiltins are Node's built-in modules (require('module').builtinModules
// in Node 24, without the names that only exist with the node: prefix).
// QuickJS provides none of them.
var nodeBuiltins = []string{
	"_http_agent", "_http_client", "_http_common", "_http_incoming", "_http_outgoing", "_http_server",
	"_stream_duplex", "_stream_passthrough", "_stream_readable", "_stream_transform", "_stream_wrap",
	"_stream_writable", "_tls_common", "_tls_wrap", "assert", "assert/strict", "async_hooks", "buffer",
	"child_process", "cluster", "console", "constants", "crypto", "dgram", "diagnostics_channel", "dns",
	"dns/promises", "domain", "events", "fs", "fs/promises", "http", "http2", "https", "inspector",
	"inspector/promises", "module", "net", "os", "path", "path/posix", "path/win32", "perf_hooks",
	"process", "punycode", "querystring", "readline", "readline/promises", "repl", "stream",
	"stream/consumers", "stream/promises", "stream/web", "string_decoder", "sys", "timers",
	"timers/promises", "tls", "trace_events", "tty", "url", "util", "util/types", "v8", "vm", "wasi",
	"worker_threads", "zlib",
}

var clientSource = regexp.MustCompile(`\.client\.[jt]sx?$`)

// SSR builds the single-file SSR bundle that QuickJS evaluates: an ES2022
// IIFE with no imports left, React's server entry trimmed to the renderer
// actually used, core-js switched to property-backed internal state, and
// *.client modules replaced by stubs. Page code that imports a Node built-in
// module fails the build.
func SSR(o SSROptions) (api.BuildResult, error) { return buildSSR(o, true) }

// buildSSR lets tests compare the trimmed React entry with the untrimmed one.
func buildSSR(o SSROptions, trimReact bool) (api.BuildResult, error) {
	opts, err := buildOptions(o.Root, o.Entry, o.Tsconfig, o.Write, nil)
	if err != nil {
		return api.BuildResult{}, err
	}
	coreJS, err := findCoreJS(opts.AbsWorkingDir)
	if err != nil {
		return api.BuildResult{}, err
	}
	opts.Plugins = append(slices.Clone(o.Plugins), ssrPlugin(trimReact, coreJS))
	opts.Outfile = o.Outfile
	opts.Platform = api.PlatformNeutral
	opts.Conditions = []string{"worker", "import"}
	opts.MainFields = []string{"module", "main"}
	opts.Format = api.FormatIIFE
	opts.External = append(slices.Clone(nodeBuiltins), "node:*")
	opts.Define["global"] = "globalThis"
	opts.Define["__SSR_PROFILE__"] = strconv.FormatBool(o.Profile)
	maps.Copy(opts.Define, o.Define)
	// Styles belong to the browser bundle. The server only renders the markup.
	opts.Loader = map[string]api.Loader{".css": api.LoaderEmpty}
	result, meta, err := run(opts)
	if err != nil {
		return result, err
	}
	for _, output := range meta.Outputs {
		if len(output.Imports) > 0 {
			imports, _ := json.Marshal(output.Imports)
			return result, fmt.Errorf("SSR bundle has unresolved runtime dependencies: %s", imports)
		}
	}
	for input := range meta.Inputs {
		if clientSource.MatchString(input) && !strings.HasPrefix(input, "client-only:") {
			return result, fmt.Errorf("client-only source leaked into SSR: %s", input)
		}
	}
	return result, nil
}

func ssrPlugin(trimReact bool, coreJS string) api.Plugin {
	return api.Plugin{Name: "tifa-ssr", Setup: func(b api.PluginBuild) {
		// esbuild calls plugin callbacks from several goroutines.
		var reactModules sync.Map
		b.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			if (strings.HasPrefix(args.Path, "node:") || slices.Contains(nodeBuiltins, args.Path)) &&
				!strings.Contains(args.Importer, "node_modules") {
				return api.OnResolveResult{Errors: []api.Message{{Text: "Node APIs are not supported in page code: " + args.Path}}}, nil
			}
			if path, ok := trimmedReactModule(&reactModules, args); trimReact && ok {
				// The reviewed renderer modules only initialize their own
				// engine. An unused engine need not run for the other's API.
				return api.OnResolveResult{Path: path, SideEffects: api.SideEffectsFalse}, nil
			}
			return api.OnResolveResult{}, nil
		})
		if trimReact {
			b.OnLoad(api.OnLoadOptions{Filter: reactServerEntry.String(), Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				return loadReactServer(&reactModules, args)
			})
		}
		// Select core-js's existing property-backed internal state. Do not
		// replace the global WeakMap or patch generated output.
		if coreJS != "" {
			coreJSAdapter(b, coreJS)
		}
		b.OnResolve(api.OnResolveOptions{Filter: `\.client(?:\.[jt]sx?)?$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			return api.OnResolveResult{Path: args.Path, Namespace: "client-only"}, nil
		})
		b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "client-only"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
			contents := `export default function ClientOnlyStub(){ throw new Error("Client-only module invoked during SSR") }`
			return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
		})
	}}
}
