// The photos of the products, the home page and the journal. Vite gives each file a URL with its
// content hash under /assets/, which the program serves with a long cache.
const files = import.meta.glob<string>('./images/**/*.avif', { eager: true, query: '?url', import: 'default' })

export type View = 'main' | 'detail' | 'scene'
export type Width = 160 | 480 | 1200

// photo returns the URL of a picture in a folder of product photos (the photo of a product, which
// products can share), or undefined when there is none.
export function photo(folder: string, view: View = 'main', width: Width = 480): string | undefined {
  return files[`./images/${folder}/${view}-${width}.avif`]
}

// photoSet is the srcset of a picture of a folder of product photos in its two larger widths.
export function photoSet(folder: string, view: View = 'main'): string | undefined {
  const small = photo(folder, view, 480)
  const large = photo(folder, view, 1200)
  return small && large ? `${small} 480w, ${large} 1200w` : undefined
}

// picture returns the URL of a picture of the home page or the journal, such as 'home/hero-1600'.
export function picture(name: string): string | undefined {
  return files[`./images/${name}.avif`]
}
