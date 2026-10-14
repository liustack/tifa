// Compiles one stylesheet with Tailwind CSS for tifa, the way Tailwind's own Vite and PostCSS
// plugins do. It runs with node -e and reads JSON from stdin:
//
//	{"cli": "<real path of @tailwindcss/cli/package.json>", "root": "<app root>", "file": "<stylesheet>", "css": "<its content>"}
//
// It loads @tailwindcss/node and @tailwindcss/oxide from where the app's @tailwindcss/cli finds
// them, writes the CSS to stdout and an error message to stderr.
const fs = require('node:fs')
const path = require('node:path')
const { createRequire } = require('node:module')

async function main() {
  const input = JSON.parse(fs.readFileSync(0, 'utf8'))
  const load = createRequire(input.cli)
  const { compile, optimize } = load('@tailwindcss/node')
  const { Scanner } = load('@tailwindcss/oxide')
  // Imported stylesheets are inlined, so their url() paths are rewritten to be relative to the
  // stylesheet's directory, where esbuild resolves them.
  const compiler = await compile(input.css, {
    base: path.dirname(input.file),
    from: input.file,
    shouldRewriteUrls: true,
    onDependency() {},
  })
  // Without source(), Tailwind scans the app root, as its CLI scans the working directory.
  const sources =
    compiler.root === 'none'
      ? []
      : compiler.root === null
        ? [{ base: input.root, pattern: '**/*', negated: false }]
        : [{ ...compiler.root, negated: false }]
  const scanner = new Scanner({ sources: sources.concat(compiler.sources) })
  const css = compiler.build(scanner.scan())
  process.stdout.write(optimize(css, { file: input.file }).code)
}

main().catch((err) => {
  process.stderr.write(String(err instanceof Error ? err.message : err))
  process.exitCode = 1
})
