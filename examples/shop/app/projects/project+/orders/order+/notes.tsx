import { zodResolver } from '@hookform/resolvers/zod'
import { useRouter } from '@tanstack/react-router'
import { useForm } from 'react-hook-form'
import { StickyNote } from 'lucide-react'
import { TifaError } from 'tifa/client'
import { z } from 'zod'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Textarea } from '@/components/ui/textarea'
import { orderTime } from '../../../../format'
import { useHydrated } from '../../../../use-hydrated'
import { layout as project } from '../../route.gen'
import { PUT, page, type PUTInput } from './route.gen'

const noteSchema = z.object({ text: z.string().trim().min(1, 'Write something').max(200, 'A note is at most 200 characters') }) satisfies z.ZodType<PUTInput>

// Notes are what the staff wrote on the order, and a form for one more. Customers never see them.
export function Notes() {
  const { order } = page.useLoaderData()
  const { timeZone } = project.useLoaderData()
  const hydrated = useHydrated()
  const router = useRouter()
  const form = useForm<PUTInput>({ resolver: zodResolver(noteSchema), defaultValues: { text: '' } })
  const notes = order.notes

  async function onSubmit(input: PUTInput) {
    try {
      await PUT({ params: { project: order.projectId, order: order.id }, input })
      await router.invalidate({ sync: true })
      form.reset()
    } catch (e) {
      if (!(e instanceof TifaError)) {
        form.setError('root', { message: 'Network error. Please try again.' })
        return
      }
      const fields = Object.entries(e.fields)
      for (const [name, message] of fields) form.setError(name as keyof PUTInput, { message })
      if (fields.length === 0) form.setError('root', { message: e.message })
    }
  }

  const { errors, isSubmitting } = form.formState
  const length = form.watch('text').length
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 font-heading text-lg font-medium tracking-[0.04em]">
          <StickyNote className="size-4 text-primary" />
          Internal notes
        </CardTitle>
        <CardAction className="font-mono text-xs text-muted-foreground">{notes.length} in all, hidden from the customer</CardAction>
      </CardHeader>
      <CardContent className="space-y-5">
        {notes.length === 0 ? (
          <p className="text-sm text-muted-foreground">No notes yet.</p>
        ) : (
          <ul className="divide-y">
            {notes.map((n) => (
              <li key={n.at} className="flex gap-3 py-3 first:pt-0">
                <Avatar className="size-8 rounded-none">
                  <AvatarFallback className="rounded-none bg-secondary text-xs text-primary">{n.author.slice(0, 1)}</AvatarFallback>
                </Avatar>
                <div className="min-w-0 flex-1">
                  <p className="flex items-baseline justify-between gap-2 text-sm">
                    <span className="font-medium">{n.author}</span>
                    <time dateTime={n.at} className="font-mono text-xs text-muted-foreground">
                      {orderTime(n.at, timeZone)}
                    </time>
                  </p>
                  <p className="text-sm text-muted-foreground">{n.text}</p>
                </div>
              </li>
            ))}
          </ul>
        )}
        <form noValidate onSubmit={form.handleSubmit(onSubmit)} className="space-y-3">
          <Field data-invalid={!!errors.text}>
            <FieldLabel htmlFor="note-text">Write a note</FieldLabel>
            <Textarea id="note-text" rows={2} placeholder="For example: delivery time confirmed with the customer" aria-invalid={!!errors.text} {...form.register('text')} />
            <FieldError errors={errors.text ? [{ message: errors.text.message }] : []} />
          </Field>
          <FieldError errors={errors.root ? [{ message: errors.root.message }] : []} />
          <div className="flex items-center justify-between">
            <span className="font-mono text-xs text-muted-foreground">{length} / 200</span>
            <Button type="submit" size="sm" disabled={!hydrated || isSubmitting}>
              {isSubmitting ? 'Saving…' : 'Add note'}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
