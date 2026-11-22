// The page writes its own og:url, which wins over the one tifa writes.
export const head = () => ({ meta: [
    { property: 'og:url', content: 'https://a.example.com/a/two-share' },
    { property: 'og:title', content: 'TWO_OG_TITLE' },
    { property: 'og:image', content: '/two-share.png' },
    { name: 'twitter:image', content: '//cdn.example.com/two.png' },
    { property: 'og:image:url', content: 'https://other.example.com/x.png' },
  ] })

export default function Page() {
  return <p>PAGE_A_TWO</p>
}
