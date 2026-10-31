export default function Desk({ children }: { children?: unknown }) {
  return <div>{children as never}</div>
}
