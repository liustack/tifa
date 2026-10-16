import type { ReactNode } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

// SummaryCard is one key number of a shop page, with a line under it.
export function SummaryCard({ title, note, children }: { title: string; note: string; children: ReactNode }) {
  return (
    <Card className="gap-1">
      <CardHeader>
        <CardDescription>{title}</CardDescription>
        <CardTitle className="text-3xl font-semibold tabular-nums">{children}</CardTitle>
      </CardHeader>
      <CardContent className="text-xs text-muted-foreground">{note}</CardContent>
    </Card>
  )
}
