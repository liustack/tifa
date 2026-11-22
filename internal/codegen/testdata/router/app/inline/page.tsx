// A component that writes document metadata itself, as React 19 allows.
export default function Page() {
  return (
    <div>
      <title>INLINE_TITLE</title>
      <meta name="description" content="INLINE_DESCRIPTION" />
      <p>PAGE_INLINE</p>
    </div>
  )
}
