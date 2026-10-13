package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
)

// The two adapters below depend on the exact source of one npm file each. A
// dependency upgrade fails the build until someone reviews the adapter again
// and updates the version and hash here.
const (
	reactDOMVersion     = "19.3.0"
	reactServerEdgeHash = "a631527404b0a7bbe10aa79db04852196fd7fd88a056c1bb84cf1a1885e1e934"
	coreJSVersion       = "3.50.0"
	internalStateHash   = "72bd626c79e840676152fbd2652e546ae5605da8542b2df0e28756d9e99fef20"
)

var (
	reactServerEntry = regexp.MustCompile(`[/\\]react-dom[/\\]server\.edge\.js$`)
	reactProduction  = regexp.MustCompile(`if \(process\.env\.NODE_ENV === 'production'\) \{([^}]+)\}`)
	reactRequire     = regexp.MustCompile(`(\w+) = require\('([^']+)'\);`)
	reactExport      = regexp.MustCompile(`exports\.(\w+) = (\w+)\.(\w+);`)
)

// loadReactServer replaces react-dom/server.edge.js, a CommonJS barrel that
// loads both the stream and the string renderer, with an ES module that
// re-exports the same public API. A named renderToString import then bundles
// only the string renderer. Application code keeps using the public React API.
// The renderer module paths are recorded in modules for trimmedReactModule.
func loadReactServer(modules *sync.Map, args api.OnLoadArgs) (api.OnLoadResult, error) {
	dir := filepath.Dir(args.Path)
	source, err := os.ReadFile(args.Path)
	if err != nil {
		return api.OnLoadResult{}, err
	}
	pkg, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return api.OnLoadResult{}, err
	}
	var manifest struct{ Version string }
	if err := json.Unmarshal(pkg, &manifest); err != nil {
		return api.OnLoadResult{}, err
	}
	contents, targets, err := reactServerExports(string(source), manifest.Version)
	if err != nil {
		return api.OnLoadResult{Errors: []api.Message{{Text: err.Error()}}}, nil
	}
	modules.Store(args.Path, targets)
	return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS, ResolveDir: dir}, nil
}

// trimmedReactModule marks the renderer modules that the rewritten
// server.edge.js imports as free of side effects, so an unused one is dropped.
func trimmedReactModule(modules *sync.Map, args api.OnResolveArgs) (string, bool) {
	targets, ok := modules.Load(args.Importer)
	if !ok || !slices.Contains(targets.([]string), args.Path) {
		return "", false
	}
	return filepath.Join(filepath.Dir(args.Importer), filepath.FromSlash(args.Path)), true
}

func reactServerExports(source, version string) (string, []string, error) {
	sum := sha256.Sum256([]byte(source))
	if version != reactDOMVersion || hex.EncodeToString(sum[:]) != reactServerEdgeHash {
		return "", nil, fmt.Errorf("Review the React SSR build adapter for react-dom %s", version)
	}
	production := reactProduction.FindStringSubmatch(source)
	if production == nil {
		return "", nil, errors.New("React SSR production entry changed")
	}
	var variables, targets []string
	modules := map[string]string{}
	for _, m := range reactRequire.FindAllStringSubmatch(production[1], -1) {
		variables = append(variables, m[1])
		targets = append(targets, m[2])
		modules[m[1]] = m[2]
	}
	exports := reactExport.FindAllStringSubmatch(source, -1)
	if len(modules) != 2 || len(exports) != 5 ||
		!slices.ContainsFunc(exports, func(m []string) bool { return m[1] == "renderToString" }) {
		return "", nil, errors.New("React SSR exports changed, review the build adapter")
	}
	var named, imports, namespace []string
	for _, m := range exports {
		name, variable, member := m[1], m[2], m[3]
		target, ok := modules[variable]
		if !ok {
			return "", nil, fmt.Errorf("Unknown React SSR implementation: %s", variable)
		}
		named = append(named, fmt.Sprintf("export { %s as %s } from %s", member, name, quote(target)))
		namespace = append(namespace, fmt.Sprintf("%s: %s.%s", name, variable, member))
	}
	for i, variable := range variables {
		imports = append(imports, fmt.Sprintf("import * as %s from %s", variable, quote(targets[i])))
	}
	// CommonJS default imports remain supported. The pure factory lets a named
	// renderToString import discard the otherwise unused default namespace.
	contents := strings.Join(named, "\n") + "\n" + strings.Join(imports, "\n") +
		"\nexport default /* @__PURE__ */ (() => ({" + strings.Join(namespace, ",") + "}))()"
	return contents, targets, nil
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// findCoreJS returns the real directory of the core-js package that the
// project root resolves, as require.resolve would from there, or "" when the
// project has none. esbuild reports module paths with symlinks resolved, so
// the adapter compares against the real directory.
func findCoreJS(root string) (string, error) {
	for dir := root; ; dir = filepath.Dir(dir) {
		pkg := filepath.Join(dir, "node_modules", "core-js")
		if _, err := os.Stat(filepath.Join(pkg, "package.json")); err == nil {
			return filepath.EvalSymlinks(pkg)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if filepath.Dir(dir) == dir {
			return "", nil
		}
	}
}

// coreJSAdapter checks the project's core-js against the reviewed version
// when esbuild loads the first file of that package, and points its WeakMap
// detection at a module that answers false. That selects core-js's own
// property-backed internal state in QuickJS.
func coreJSAdapter(b api.PluginBuild, root string) {
	var once sync.Once
	b.OnLoad(api.OnLoadOptions{Filter: "^" + regexp.QuoteMeta(root) + `[/\\]`, Namespace: "file"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
		var err error
		once.Do(func() { err = checkCoreJS(root) })
		if err != nil {
			// Only the first load reports the failed check. The build already fails.
			return api.OnLoadResult{Errors: []api.Message{{Text: err.Error()}}}, nil
		}
		return api.OnLoadResult{}, nil
	})
	state := filepath.Join(root, "internals", "internal-state.js")
	b.OnResolve(api.OnResolveOptions{Filter: `^\.\./internals/weak-map-basic-detection$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
		if args.Importer != state {
			return api.OnResolveResult{}, nil
		}
		return api.OnResolveResult{Path: "property-state", Namespace: "quickjs-core-js"}, nil
	})
	b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "quickjs-core-js"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
		contents := "module.exports = false"
		return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
	})
}

func checkCoreJS(root string) error {
	var manifest struct{ Version string }
	pkg, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err == nil {
		err = json.Unmarshal(pkg, &manifest)
	}
	var state []byte
	if err == nil {
		state, err = os.ReadFile(filepath.Join(root, "internals", "internal-state.js"))
	}
	sum := sha256.Sum256(state)
	if err != nil || manifest.Version != coreJSVersion || hex.EncodeToString(sum[:]) != internalStateHash {
		return fmt.Errorf("Review the QuickJS core-js compatibility adapter after upgrading core-js (found %s at %s)", manifest.Version, root)
	}
	return nil
}
