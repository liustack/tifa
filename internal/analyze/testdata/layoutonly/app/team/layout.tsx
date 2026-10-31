export default function Team({ children }: { children?: unknown }) {
  return <div>{children as never}</div>
}
