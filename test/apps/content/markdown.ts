// The rehype plugin of the app, which tifa runs on the HTML of each body: images load when they
// come into view.
type Node = { type: string; tagName?: string; properties?: Record<string, unknown>; children?: Node[] }

export function rehypeLazyImages() {
  const visit = (node: Node) => {
    if (node.type === 'element' && node.tagName === 'img') node.properties = { ...node.properties, loading: 'lazy' }
    node.children?.forEach(visit)
  }
  return visit
}
