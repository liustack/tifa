package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// lucidePage renders lucide-react icons with every kind of prop the icon cache tells apart, three
// times so that later renders find what the first left in the cache, then once each with a
// built-in lucide calls replaced by an equal one and by one that gives other results. Probe
// renders an icon twice through Icon's render function and reports whether both share the
// elements of the icon's paths, which only the cache makes them do.
const lucidePage = `import './host'
import { renderToString } from 'react-dom/server.edge'
import { House, Star, Search, Icon, LucideProvider, createLucideIcon } from 'lucide-react'

const Custom = createLucideIcon('custom-thing', [['circle', { cx: 12, cy: 12, r: 4, key: 'c' }]], ['thing'])
const node = [['rect', { x: 1, y: 1, width: 4, height: 4, key: 'r' }]]

function Page() {
  return (
    <div>
      <House />
      <House className="size-4" />
      <House className="size-4" />
      <House className="  a  b a " />
      <House className="" />
      <Star fill="currentColor" aria-hidden="true" />
      <Star aria-hidden="true" fill="currentColor" />
      <Star fill="none" data-x={1} />
      <Star fill="none" data-x="1" />
      <Star fill="none" data-x={true} />
      <Search size={16} />
      <Search size="16" />
      <Search width={-0} height={0} absoluteStrokeWidth strokeWidth={3} />
      <Search width={0} height={0} absoluteStrokeWidth strokeWidth={3} />
      <Search width={NaN} />
      <Search color="red" strokeWidth={1.5} nonScalingStroke />
      <Search color={null} strokeWidth={undefined} />
      <Search style={{ color: 'blue' }} />
      <Search onClick={() => {}} />
      <Search title="find" />
      <Search role="img" />
      <Search aria-label="find">child</Search>
      <Search>{['a', 'b']}</Search>
      <Search>{0}</Search>
      <Custom />
      <Custom className="x" />
      <Icon iconNode={node} />
      <Icon iconNode={node} className="x" />
      <LucideProvider size={20} color="green" strokeWidth={1} className="ctx">
        <House />
        <House className="ctx" />
      </LucideProvider>
      <LucideProvider absoluteStrokeWidth nonScalingStroke>
        <Star size={48} />
      </LucideProvider>
      {Array.from({ length: 40 }, (_, i) => (
        <House key={i} className={'c' + (i % 20)} />
      ))}
    </div>
  )
}

const probes = []
function Probe() {
  const element = Icon.render(House.render({ className: 'probe' }, null).props, null)
  probes.push(element)
  return element
}

const html = []
for (let i = 0; i < 3; i++) html.push(renderToString(<Page />))
const trim = String.prototype.trim
String.prototype.trim = function () {
  return trim.call(this)
}
html.push(renderToString(<Page />))
String.prototype.trim = trim
const join = Array.prototype.join
Array.prototype.join = function (separator) {
  return join.call(this, separator === ' ' ? ' ~ ' : separator)
}
html.push(renderToString(<Page />))
Array.prototype.join = join
html.push(renderToString(<Page />))
renderToString(<Probe />)
renderToString(<Probe />)
globalThis.result = JSON.stringify({ html, shared: probes[0].props.children[0] === probes[1].props.children[0] })
`

type lucideResult struct {
	HTML   []string
	Shared bool
}

func lucideRender(t *testing.T, root string) (lucideResult, string) {
	t.Helper()
	writeTree(t, root, map[string]string{"lucide.tsx": lucidePage})
	code, printed := buildPrinting(t, root, "lucide.tsx")
	var r lucideResult
	if err := json.Unmarshal([]byte(evalResult(t, code)), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.HTML) != 6 {
		t.Fatalf("%d pages, want 6", len(r.HTML))
	}
	return r, printed
}

// The tifa plugin caches what lucide-react builds for an icon with the same props, in the SSR
// bundle. Pages render the same as with lucide-react as it is, whose files the plugin does not
// know when one of them changed: it then warns once and leaves lucide-react alone.
func TestSSRCachesLucideIcons(t *testing.T) {
	cached, printed := lucideRender(t, copyTestdata(t, "ssr", true))
	if strings.Contains(printed, "lucide-react 1.58.0") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if !cached.Shared {
		t.Error("two renders of an icon with the same props do not share its path elements")
	}
	for _, file := range []string{"Icon.mjs", filepath.Join("shared", "src", "utils", "mergeClasses.mjs")} {
		changed := withChangedPackage(t, "lucide-react", filepath.Join("dist", "esm", file), func(source string) string {
			return source + "\n// changed\n"
		})
		plain, printed := lucideRender(t, changed)
		if strings.Count(printed, "lucide-react 1.58.0") != 1 || !strings.Contains(printed, "the tifa plugin does not know") {
			t.Errorf("%s changed: the build prints:\n%s", file, printed)
		}
		if plain.Shared {
			t.Errorf("%s changed: renders share the path elements of an icon", file)
		}
		for i := range plain.HTML {
			if cached.HTML[i] != plain.HTML[i] {
				t.Errorf("%s changed: page %d differs:\n%s\nwant\n%s", file, i, cached.HTML[i], plain.HTML[i])
			}
		}
	}
	// The page renders what each prop asks for.
	for _, want := range []string{`class="lucide lucide-house lucide-home size-4"`, `class="lucide lucide-house lucide-home a b"`, `fill="currentColor"`, `data-x="1"`, `vector-effect="non-scaling-stroke"`, `lucide-custom-thing lucide-thing`} {
		if !strings.Contains(cached.HTML[0], want) {
			t.Errorf("page has no %s:\n%s", want, cached.HTML[0])
		}
	}
	if cached.HTML[4] == cached.HTML[5] {
		t.Error("the page with Array.prototype.join replaced renders like the others")
	}
}
