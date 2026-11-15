package bundle

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// rehypeHighlightPage runs rehype-highlight on small trees of code blocks under every option it
// reads, and records the tree it gives for each block with the messages it wrote. Each scenario
// also records how many highlight.js instances it made: rehypeHighlight makes one with
// createLowlight on every call, which the tifa plugin lets it do once for each languages object.
// HLJS is the path of highlight.js's core, which lowlight makes its instances with.
const rehypeHighlightPage = `import './host'
import rehypeHighlight from 'rehype-highlight'
import hljs from 'HLJS'

let instances = 0
const newInstance = hljs.newInstance
hljs.newInstance = function () {
  instances++
  return newInstance.apply(this, arguments)
}

const samples: Record<string, string> = {
  javascript: 'const a = 1\nfunction f(x) { return x + "s" }\n',
  python: 'def f(x):\n    return [i for i in range(x)]\n',
  css: 'a.b > c { color: #fff; margin: 0 auto }\n',
  xml: '<div class="a">text</div>\n',
  bash: 'echo "$HOME" | grep -v x\n',
  tiny: 'foo bar baz\n',
  extra: 'foo bar baz\n',
}
function tree(lang: string | undefined, text?: string) {
  const className = lang ? ['language-' + lang] : []
  return {
    type: 'root',
    children: [
      {
        type: 'element', tagName: 'pre', properties: {},
        children: [
          {
            type: 'element', tagName: 'code', properties: { className },
            children: [{ type: 'text', value: text ?? samples[lang ?? 'javascript'] ?? 'foo bar baz\n' }],
          },
        ],
      },
    ],
  }
}
function transform(t: any, lang: string | undefined, text?: string) {
  const messages: string[] = []
  const root = tree(lang, text)
  t(root, { message: (m: string, o: any) => messages.push(m + ' ' + o.ruleId) })
  return { root, messages }
}

const scenarios: { name: string; out: unknown; instances: number }[] = []
function scenario(name: string, run: () => unknown) {
  const before = instances
  scenarios.push({ name, out: run(), instances: 0 })
  scenarios[scenarios.length - 1].instances = instances - before
}
const langs = ['javascript', 'python', 'css', 'xml', 'bash']

scenario('default, 30 calls', () => {
  const out: unknown[] = []
  for (let i = 0; i < 30; i++) out.push(transform(rehypeHighlight(), langs[i % 5]))
  return out
})
scenario('default options object', () => langs.map((l) => transform(rehypeHighlight({}), l)))
scenario('detect', () => [transform(rehypeHighlight({ detect: true }), undefined, samples.python), transform(rehypeHighlight({ detect: true }), undefined, samples.css), transform(rehypeHighlight(), undefined)])
scenario('subset', () => [
  transform(rehypeHighlight({ detect: true, subset: ['python', 'css'] }), undefined, samples.python),
  transform(rehypeHighlight({ detect: true, subset: ['xml'] }), undefined, samples.javascript),
])
scenario('plainText', () => [transform(rehypeHighlight({ plainText: ['python'] }), 'python'), transform(rehypeHighlight({ plainText: ['python'] }), 'css'), transform(rehypeHighlight(), 'python')])
scenario('prefix in turn', () => {
  const a = rehypeHighlight({ prefix: 'a-' }), b = rehypeHighlight({ prefix: 'b-' }), c = rehypeHighlight(), d = rehypeHighlight({ prefix: 'zed' })
  return [a, b, a, c, b, d, a, c].map((t, i) => transform(t, langs[i % 5]))
})
scenario('missing language', () => [transform(rehypeHighlight(), 'nonesuch'), transform(rehypeHighlight(), 'javascript')])
scenario('aliases then none', () => [
  transform(rehypeHighlight({ aliases: { javascript: ['jsx2'] } }), 'jsx2', samples.javascript),
  transform(rehypeHighlight({ aliases: { javascript: ['jsx2'] } }), 'jsx2', samples.javascript),
  transform(rehypeHighlight(), 'jsx2', samples.javascript),
  transform(rehypeHighlight({ aliases: {} }), 'jsx2', samples.javascript),
  transform(rehypeHighlight({ aliases: { python: 'py2' } }), 'py2'),
  transform(rehypeHighlight(), 'py2'),
])
const tiny = () => ({ name: 'tiny', keywords: 'foo bar' })
const tiny2 = () => ({ name: 'tiny', keywords: 'baz' })
const extra = () => ({ name: 'extra', keywords: 'baz' })
scenario('custom languages', () => {
  const custom = { tiny }
  return [transform(rehypeHighlight({ languages: custom }), 'tiny'), transform(rehypeHighlight({ languages: custom }), 'tiny'), transform(rehypeHighlight({ languages: custom }), 'javascript'), transform(rehypeHighlight(), 'tiny')]
})
scenario('languages changed between calls', () => {
  const custom: Record<string, any> = { tiny }
  const out = [transform(rehypeHighlight({ languages: custom }), 'tiny')]
  custom.extra = extra
  out.push(transform(rehypeHighlight({ languages: custom }), 'extra'), transform(rehypeHighlight({ languages: custom }), 'extra'))
  custom.tiny = tiny2
  out.push(transform(rehypeHighlight({ languages: custom }), 'tiny'))
  delete custom.extra
  out.push(transform(rehypeHighlight({ languages: custom }), 'extra'), transform(rehypeHighlight({ languages: custom }), 'tiny'))
  const reordered = { extra: custom.extra ?? extra, tiny: custom.tiny }
  const again: Record<string, any> = { tiny: custom.tiny }
  again.extra = extra
  delete again.tiny
  again.tiny = tiny2
  out.push(transform(rehypeHighlight({ languages: again }), 'tiny'), transform(rehypeHighlight({ languages: reordered }), 'tiny'))
  return out
})
scenario('languages that registers a getter', () => {
  let n = 0
  const custom = {} as Record<string, any>
  Object.defineProperty(custom, 'tiny', { enumerable: true, get: () => (n++ % 2 ? tiny2 : tiny) })
  return [0, 1, 2].map(() => transform(rehypeHighlight({ languages: custom }), 'tiny'))
})
const kept = scenarios.find((s) => s.name === 'default, 30 calls')!
globalThis.result = JSON.stringify({ scenarios, kept: kept.instances })
`

type rehypeHighlightResult struct {
	Scenarios []struct {
		Name      string
		Out       json.RawMessage
		Instances int
	}
	Kept int
}

// highlightJSCore is the real path of highlight.js's core, which rehype-highlight's lowlight makes
// its instances with, so that a page can count them.
func highlightJSCore(t *testing.T) string {
	t.Helper()
	rehype, err := filepath.EvalSymlinks(filepath.Join(testModules(t), "rehype-highlight"))
	if err != nil {
		t.Fatal(err)
	}
	lowlight, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(rehype), "lowlight"))
	if err != nil {
		t.Fatal(err)
	}
	hljs, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(lowlight), "highlight.js"))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(filepath.Join(hljs, "lib", "core.js"))
}

func rehypeHighlightRender(t *testing.T, root string) (rehypeHighlightResult, string) {
	t.Helper()
	page := strings.Replace(rehypeHighlightPage, "'HLJS'", "'"+highlightJSCore(t)+"'", 1)
	writeTree(t, root, map[string]string{"rehype-highlight.tsx": page})
	code, printed := buildPrinting(t, root, "rehype-highlight.tsx")
	var r rehypeHighlightResult
	if err := json.Unmarshal([]byte(evalResult(t, code)), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Scenarios) != 11 {
		t.Fatalf("%d scenarios, want 11", len(r.Scenarios))
	}
	return r, printed
}

// rehypeHighlightUnknown is a copy of rehype-highlight whose createLowlight line has a form the
// plugin does not know.
func rehypeHighlightUnknown(t *testing.T) string {
	t.Helper()
	return withChangedPackage(t, "rehype-highlight", filepath.Join("lib", "index.js"), func(source string) string {
		const old = "const lowlight = createLowlight(languages)"
		if strings.Count(source, old) != 1 {
			t.Fatalf("rehype-highlight does not have %q once", old)
		}
		return strings.Replace(source, old, "const lowlight = createLowlight(languages, 1)", 1)
	})
}

// rehype-highlight makes a lowlight, and so a highlight.js instance with the grammars compiled
// again, on every call, and react-markdown calls it on every render. The tifa plugin keeps one for
// each languages object. The trees it gives must be those of the original for every option, and
// after the languages object changed.
func TestSSRKeepsOneLowlightForRehypeHighlight(t *testing.T) {
	changed, printed := rehypeHighlightRender(t, copyTestdata(t, "ssr", true))
	if strings.Contains(printed, "rehype-highlight 7.0.2") {
		t.Errorf("the build prints:\n%s", printed)
	}
	plain, printed := rehypeHighlightRender(t, rehypeHighlightUnknown(t))
	if strings.Count(printed, "rehype-highlight 7.0.2") != 1 {
		t.Fatalf("the build of the changed package prints:\n%s", printed)
	}
	if changed.Kept != 1 {
		t.Errorf("30 calls with the default languages made %d instances, want 1", changed.Kept)
	}
	if plain.Kept != 30 {
		t.Errorf("the original made %d instances for 30 calls, want 30", plain.Kept)
	}
	for i, s := range changed.Scenarios {
		p := plain.Scenarios[i]
		if s.Name != p.Name {
			t.Fatalf("scenario %d is %q and %q", i, s.Name, p.Name)
		}
		var a, b any
		if err := json.Unmarshal(s.Out, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(p.Out, &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: the trees differ\nwith the rewrite: %s\noriginal:         %s", s.Name, s.Out, p.Out)
		}
		if s.Instances > p.Instances {
			t.Errorf("%s: %d instances, the original made %d", s.Name, s.Instances, p.Instances)
		}
	}
	byName := map[string]int{}
	for _, s := range changed.Scenarios {
		byName[s.Name] = s.Instances
	}
	// A changed languages object, and the aliases, still make their own instances.
	if byName["aliases then none"] != 4 {
		t.Errorf("calls with aliases made %d instances, want one for each of the 4 calls that set aliases", byName["aliases then none"])
	}
	if byName["languages changed between calls"] != 6 {
		t.Errorf("calls after a change of the languages made %d instances, want 6", byName["languages changed between calls"])
	}
	// The trees differ from each other, so the comparison above has something to compare: the
	// prefix of each call is its own, and the alias reaches only the call that sets it.
	text := string(changed.Scenarios[5].Out)
	for _, want := range []string{`"a-`, `"b-`, `"hljs-`, `"zed`} {
		if !strings.Contains(text, want) {
			t.Errorf("prefix scenario has no %s in:\n%s", want, text)
		}
	}
	if !strings.Contains(string(changed.Scenarios[7].Out), "missing-language") {
		t.Error("an alias of one call reached the call that has none")
	}
}

// A release of rehype-highlight whose createLowlight line differs is left alone, with one warning.
func TestSSRWarnsOfAnUnknownRehypeHighlight(t *testing.T) {
	got, printed := rehypeHighlightRender(t, rehypeHighlightUnknown(t))
	want := notRecognized + "rehype-highlight 7.0.2: "
	if strings.Count(printed, want) != 1 || !strings.Contains(printed, "the tifa plugin knows") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if got.Kept != 30 {
		t.Errorf("30 calls made %d instances, want 30 for the original", got.Kept)
	}
}
