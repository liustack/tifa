import { zodResolver } from '@hookform/resolvers/zod'
import { PencilLine } from 'lucide-react'
import { useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { TifaError } from 'tifa/client'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { useHydrated } from '../../../../use-hydrated'
import { PATCH, route, type PATCHInput } from './+route'

// The same rules as orders.CheckRecipient in Go, which checks again on the server.
const recipientSchema = z.object({
  customer: z.string().trim().min(1, '请填写收件人'),
  phone: z
    .string()
    .transform((s) => s.replace(/[ -]/g, ''))
    .pipe(z.string().regex(/^1[3-9][0-9]{9}$/, '请填写 11 位手机号')),
  address: z
    .string()
    .trim()
    .min(1, '请填写收货地址')
    // Count characters as Go does, not UTF-16 units.
    .refine((s) => [...s].length <= 100, '收货地址最多 100 个字'),
}) satisfies z.ZodType<PATCHInput, PATCHInput>

export function RecipientDialog() {
  const { order } = route.useLoaderData()
  const hydrated = useHydrated()
  const [open, setOpen] = useState(false)
  const current = { customer: order.customer, phone: order.phone, address: order.address }
  const form = useForm<PATCHInput>({ resolver: zodResolver(recipientSchema), defaultValues: current })

  function openChange(next: boolean) {
    // Start from what the order says now, not from an edit that was cancelled.
    if (next) form.reset(current)
    setOpen(next)
  }

  async function onSubmit(input: PATCHInput) {
    try {
      await PATCH({ params: { project: order.projectId, order: order.id }, input })
      setOpen(false)
    } catch (e) {
      if (!(e instanceof TifaError)) {
        form.setError('root', { message: '网络异常，请重试' })
        return
      }
      const fields = Object.entries(e.fields)
      for (const [name, message] of fields) form.setError(name as keyof PATCHInput, { message })
      if (fields.length === 0) form.setError('root', { message: e.message })
    }
  }

  const { isSubmitting, errors } = form.formState
  return (
    <Dialog open={open} onOpenChange={openChange}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" disabled={!hydrated || order.status !== 'pending'}>
          <PencilLine />
          修改收货信息
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <form noValidate onSubmit={form.handleSubmit(onSubmit)}>
          <DialogHeader className="mb-4">
            <DialogTitle>修改收货信息</DialogTitle>
            <DialogDescription>发货前都可以修改，发货后以运单为准。</DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Controller
              name="customer"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor="recipient-customer">收件人</FieldLabel>
                  <Input {...field} id="recipient-customer" autoComplete="name" aria-invalid={fieldState.invalid} />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            <Controller
              name="phone"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor="recipient-phone">手机号</FieldLabel>
                  <Input
                    {...field}
                    id="recipient-phone"
                    type="tel"
                    inputMode="numeric"
                    autoComplete="tel"
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            <Controller
              name="address"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor="recipient-address">收货地址</FieldLabel>
                  <Textarea
                    {...field}
                    id="recipient-address"
                    rows={3}
                    autoComplete="street-address"
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            {errors.root && <FieldError>{errors.root.message}</FieldError>}
          </FieldGroup>
          <DialogFooter className="mt-6">
            <DialogClose asChild>
              <Button type="button" variant="outline">
                取消
              </Button>
            </DialogClose>
            <Button type="submit" disabled={isSubmitting}>
              {isSubmitting ? '正在保存…' : '保存'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
