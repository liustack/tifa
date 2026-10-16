import { zodResolver } from '@hookform/resolvers/zod'
import { Controller, useForm } from 'react-hook-form'
import { TifaError } from 'tifa/client'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useHydrated } from '../../../../use-hydrated'
import { POST, route, type POSTInput } from './+route'

// The browser checks what it can before sending. The order page checks again with the same rules
// in Go, and which carriers exist is up to Go alone.
const shipSchema = z.object({
  carrier: z.string().min(1, '请选择快递公司'),
  trackingNo: z
    .string()
    .trim()
    .regex(/^[A-Za-z0-9]{8,20}$/, '运单号是 8 到 20 位字母或数字'),
}) satisfies z.ZodType<POSTInput>

export function ShipForm() {
  const { order, carriers } = route.useLoaderData()
  const hydrated = useHydrated()
  const form = useForm<POSTInput>({
    resolver: zodResolver(shipSchema),
    defaultValues: { carrier: '', trackingNo: '' },
  })

  async function onSubmit(input: POSTInput) {
    try {
      // The page and its Layouts load their data again before this resolves, so the order shows
      // as shipped and this form goes away.
      await POST({ params: { project: order.projectId, order: order.id }, input })
    } catch (e) {
      if (!(e instanceof TifaError)) {
        form.setError('root', { message: '网络异常，请重试' })
        return
      }
      const fields = Object.entries(e.fields)
      for (const [name, message] of fields) form.setError(name as keyof POSTInput, { message })
      if (fields.length === 0) form.setError('root', { message: e.message })
    }
  }

  const { isSubmitting, errors } = form.formState
  return (
    <Card>
      <CardHeader>
        <CardTitle>发货</CardTitle>
        <CardDescription>填好运单，客户会收到发货通知。</CardDescription>
      </CardHeader>
      <CardContent>
        <form noValidate onSubmit={form.handleSubmit(onSubmit)}>
          <FieldGroup>
            <Controller
              name="carrier"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor="ship-carrier">快递公司</FieldLabel>
                  <Select name={field.name} value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger
                      id="ship-carrier"
                      className="w-full"
                      aria-invalid={fieldState.invalid}
                      onBlur={field.onBlur}
                    >
                      <SelectValue placeholder="选择快递公司" />
                    </SelectTrigger>
                    <SelectContent>
                      {(carriers ?? []).map((c) => (
                        <SelectItem key={c.code} value={c.code}>
                          {c.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            <Controller
              name="trackingNo"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor="ship-tracking">运单号</FieldLabel>
                  <Input
                    {...field}
                    id="ship-tracking"
                    autoComplete="off"
                    placeholder="例如 SF1234567890"
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            {errors.root && <FieldError>{errors.root.message}</FieldError>}
            <Button type="submit" disabled={!hydrated || isSubmitting}>
              {isSubmitting ? '正在发货…' : '确认发货'}
            </Button>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}
