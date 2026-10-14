package bundle

import (
	"fmt"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// esVersions are the ECMAScript versions build.target accepts, as esbuild names them.
var esVersions = map[string]api.Target{
	"es5": api.ES5, "es6": api.ES2015, "es2015": api.ES2015, "es2016": api.ES2016, "es2017": api.ES2017,
	"es2018": api.ES2018, "es2019": api.ES2019, "es2020": api.ES2020, "es2021": api.ES2021,
	"es2022": api.ES2022, "es2023": api.ES2023, "es2024": api.ES2024, "esnext": api.ESNext,
}

// engines are the browsers build.target may name with a version, such as safari14.
var engines = map[string]api.EngineName{
	"chrome": api.EngineChrome, "edge": api.EngineEdge, "firefox": api.EngineFirefox,
	"ios": api.EngineIOS, "opera": api.EngineOpera, "safari": api.EngineSafari,
}

// setTarget applies build.target to the browser build: a comma-separated list of one
// ECMAScript version and browsers with versions, as esbuild's --target takes it, such as
// "es2020" or "es2019,chrome90,safari14". Empty keeps the target of buildOptions.
func setTarget(opts *api.BuildOptions, target string) error {
	if strings.TrimSpace(target) == "" {
		return nil
	}
	opts.Target = api.DefaultTarget
	for _, item := range strings.Split(target, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if v, ok := esVersions[item]; ok {
			opts.Target = v
			continue
		}
		name := strings.TrimRight(item, "0123456789.")
		engine, ok := engines[name]
		if !ok || name == item {
			return fmt.Errorf("build.target: %q is neither an ECMAScript version (es2020, esnext) nor a browser with a version (chrome90, safari14)", item)
		}
		opts.Engines = append(opts.Engines, api.Engine{Name: engine, Version: item[len(name):]})
	}
	return nil
}
