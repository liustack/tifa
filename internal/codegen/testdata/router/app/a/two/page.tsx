// The page writes its own og:url, which wins over the one tifa writes.
export const head = () => ({ meta: [{ property: 'og:url', content: 'https://a.example.com/a/two-share' }] })

export default function Page() {
  return <p>PAGE_A_TWO</p>
}
