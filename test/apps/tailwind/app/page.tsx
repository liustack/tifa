import { Badge } from '@/components/badge'

export default function Home() {
  return (
    <main className="mx-auto max-w-2xl p-6">
      <h1 className="text-3xl font-bold tracking-tight">Release notes</h1>
      <Badge>new</Badge>
    </main>
  )
}

export const head = () => ({ meta: [{ title: 'Release notes' }] })
