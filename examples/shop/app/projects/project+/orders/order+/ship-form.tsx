import { zodResolver } from '@hookform/resolvers/zod'
import { useRouter } from '@tanstack/react-router'
import { Controller, useForm } from 'react-hook-form'
import { TifaError } from 'tifa/client'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useHydrated } from '../../../../use-hydrated'
import { POST, page, type POSTInput } from './route.gen'

// The browser checks what it can before sending. The order page checks again with the same rules
// in Go, and which carriers exist is up to Go alone.
const shipSchema = z.object({
  carrier: z.string().min(1, 'Pick a carrier'),
  trackingNo: z
    .string()
    .trim()
    .regex(/^[A-Za-z0-9]{8,20}$/, 'The tracking number is 8 to 20 letters or digits'),
}) satisfies z.ZodType<POSTInput>

export function ShipForm() {
  const { order, carriers } = page.useLoaderData()
  const hydrated = useHydrated()
  const router = useRouter()
  const form = useForm<POSTInput>({
    resolver: zodResolver(shipSchema),
    defaultValues: { carrier: '', trackingNo: '' },
  })

  async function onSubmit(input: POSTInput) {
    try {
      await POST({ params: { project: order.projectId, order: order.id }, input })
      // Load the page and its Layouts again, so the order shows as shipped and this form goes away.
      await router.invalidate({ sync: true })
    } catch (e) {
      if (!(e instanceof TifaError)) {
        form.setError('root', { message: 'Network error. Please try again.' })
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
        <CardTitle>Ship</CardTitle>
        <CardDescription>Pick a carrier and enter the tracking number.</CardDescription>
      </CardHeader>
      <CardContent>
        <form noValidate onSubmit={form.handleSubmit(onSubmit)}>
          <FieldGroup>
            <Controller
              name="carrier"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor="ship-carrier">Carrier</FieldLabel>
                  <Select name={field.name} value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger
                      id="ship-carrier"
                      className="w-full"
                      aria-invalid={fieldState.invalid}
                      onBlur={field.onBlur}
                    >
                      <SelectValue placeholder="Pick a carrier" />
                    </SelectTrigger>
                    <SelectContent>
                      {carriers.map((c) => (
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
                  <FieldLabel htmlFor="ship-tracking">Tracking number</FieldLabel>
                  <Input
                    {...field}
                    id="ship-tracking"
                    autoComplete="off"
                    placeholder="For example SF1234567890"
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            {errors.root && <FieldError>{errors.root.message}</FieldError>}
            <Button type="submit" disabled={!hydrated || isSubmitting}>
              {isSubmitting ? 'Shipping…' : 'Confirm shipment'}
            </Button>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}
