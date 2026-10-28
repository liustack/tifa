// The photos of the products, the home page and the journal. Vite gives each file a URL with its
// content hash under /assets/, which the program serves with a long cache.
const files = import.meta.glob<string>('./images/**/*.avif', { eager: true, query: '?url', import: 'default' })

export type View = 'main' | 'detail' | 'scene'
export type Width = 160 | 480 | 1200

// photo returns the URL of a product's picture, or undefined when the product has none.
export function photo(slug: string, view: View = 'main', width: Width = 480): string | undefined {
  return files[`./images/${slug}/${view}-${width}.avif`]
}

// photoSet is the srcset of a product's picture in its two larger widths.
export function photoSet(slug: string, view: View = 'main'): string | undefined {
  const small = photo(slug, view, 480)
  const large = photo(slug, view, 1200)
  return small && large ? `${small} 480w, ${large} 1200w` : undefined
}

// picture returns the URL of a picture of the home page or the journal, such as 'home/hero-1600'.
export function picture(name: string): string | undefined {
  return files[`./images/${name}.avif`]
}
