import { zodResolver } from '@hookform/resolvers/zod'
import { Controller, useForm } from 'react-hook-form'
import { TifaError } from 'tifa/client'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useHydrated } from '../use-hydrated'
import { POST, page } from './route.gen'

export const head = () => ({ meta: [{ title: '登录 · 山海工作台' }] })

const schema = z.object({
  email: z.string().trim().min(1, '请填写邮箱'),
  password: z.string().min(1, '请填写密码'),
})

type Form = z.infer<typeof schema>

export default function LoginPage() {
  const { next } = page.useLoaderData()
  const hydrated = useHydrated()
  const form = useForm<Form>({ resolver: zodResolver(schema), defaultValues: { email: '', password: '' } })

  async function onSubmit(input: Form) {
    try {
      // The server answers with a redirect to the page the visitor came from.
      await POST({ input: { ...input, next } })
    } catch (e) {
      form.setError('root', { message: e instanceof TifaError ? e.message : '网络异常，请重试' })
    }
  }

  const { isSubmitting, errors } = form.formState
  return (
    <main className="flex min-h-svh items-center justify-center bg-muted/40 px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center gap-3">
          <span className="flex size-10 items-center justify-center bg-primary font-heading text-xl text-primary-foreground">山</span>
          <div className="grid leading-tight">
            <span className="font-heading text-lg font-medium tracking-[0.06em]">山海</span>
            <span className="text-xs text-muted-foreground">店铺工作台</span>
          </div>
        </div>
        <Card className="rounded-none">
          <CardHeader>
            <CardTitle>登录</CardTitle>
            <CardDescription>登录后查看订单和统计。</CardDescription>
          </CardHeader>
          <CardContent>
            <form id="login-form" noValidate onSubmit={form.handleSubmit(onSubmit)}>
              <FieldGroup>
                <Controller
                  name="email"
                  control={form.control}
                  render={({ field, fieldState }) => (
                    <Field data-invalid={fieldState.invalid}>
                      <FieldLabel htmlFor="email">邮箱</FieldLabel>
                      <Input {...field} id="email" type="email" autoComplete="username" aria-invalid={fieldState.invalid} />
                      {fieldState.error && <FieldError errors={[fieldState.error]} />}
                    </Field>
                  )}
                />
                <Controller
                  name="password"
                  control={form.control}
                  render={({ field, fieldState }) => (
                    <Field data-invalid={fieldState.invalid}>
                      <FieldLabel htmlFor="password">密码</FieldLabel>
                      <Input {...field} id="password" type="password" autoComplete="current-password" aria-invalid={fieldState.invalid} />
                      {fieldState.error && <FieldError errors={[fieldState.error]} />}
                    </Field>
                  )}
                />
                {errors.root && <FieldError errors={[errors.root]} />}
                <Button type="submit" disabled={!hydrated || isSubmitting}>
                  {isSubmitting ? '登录中' : '登录'}
                </Button>
              </FieldGroup>
            </form>
          </CardContent>
          <CardFooter className="grid gap-1 border-t text-xs text-muted-foreground">
            <span>演示账号 owner@shanhai.example</span>
            <span>密码 shanhai-demo</span>
          </CardFooter>
        </Card>
      </div>
    </main>
  )
}
