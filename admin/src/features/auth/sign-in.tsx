import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { BRAND } from '@/config/navigation'
import { errorMessage } from '@/lib/http'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@/components/ui/input-otp'
import { Label } from '@/components/ui/label'
import { PasswordInput } from '@/components/password-input'
import { authApi, meQuery, type MeResult } from './api'

const credentialsSchema = z.object({
  username: z.string().trim().min(3, '用户名至少 3 个字符'),
  password: z.string().min(8, '密码至少 8 位'),
})

type Credentials = z.infer<typeof credentialsSchema>

export function SignIn() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { redirect } = useSearch({ from: '/(auth)/login' })
  const setSession = useAuthStore((s) => s.setSession)
  const [mfaTicket, setMfaTicket] = useState<string | null>(null)
  const [mfaMethods, setMfaMethods] = useState<Array<'totp' | 'passkey'>>([])
  const [code, setCode] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const form = useForm<Credentials>({
    resolver: zodResolver(credentialsSchema),
    defaultValues: { username: '', password: '' },
  })

  const finish = async (session: MeResult) => {
    setSession(session.user, session.csrf_token, session.permissions)
    queryClient.setQueryData(meQuery.queryKey, session)
    // 只允许站内跳转，防止开放重定向
    const target = redirect?.startsWith('/') && !redirect.startsWith('//') ? redirect : '/'
    await navigate({ to: target, replace: true })
  }

  const onSubmit = form.handleSubmit(async ({ username, password }) => {
    setSubmitting(true)
    try {
      const result = await authApi.login(username, password)
      if (result.mfa_required) {
        setMfaTicket(result.mfa_ticket)
        setMfaMethods(result.mfa_methods)
        return
      }
      await finish({ user: result.user, csrf_token: result.csrf_token, permissions: result.permissions })
    } catch (error) {
      toast.error(errorMessage(error))
    } finally {
      setSubmitting(false)
    }
  })

  const onVerify = async () => {
    if (!mfaTicket || code.length !== 6) return
    setSubmitting(true)
    try {
      await finish(await authApi.verifyTotp(mfaTicket, code))
    } catch (error) {
      setCode('')
      toast.error(errorMessage(error))
    } finally {
      setSubmitting(false)
    }
  }

  const onPasskey = async () => {
    if (!mfaTicket) return
    setSubmitting(true)
    try {
      await finish(await authApi.loginPasskey(mfaTicket))
    } catch (error) {
      toast.error(errorMessage(error))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className='flex min-h-svh items-center justify-center bg-muted/40 p-4'>
      <Card className='w-full max-w-sm'>
        <CardHeader className='text-center'>
          <CardTitle className='text-xl'>{BRAND.name}</CardTitle>
          <CardDescription>仅限授权人员 · 所有操作记入审计日志</CardDescription>
        </CardHeader>
        <CardContent>
          {mfaTicket ? (
            <div className='flex flex-col items-center gap-4'>
              {mfaMethods.includes('totp') && (
                <>
                  <p className='text-sm text-muted-foreground'>请输入身份验证应用中的 6 位验证码</p>
                  <InputOTP
                    maxLength={6}
                    value={code}
                    onChange={setCode}
                    onComplete={onVerify}
                    aria-label='二次验证码'
                  >
                    <InputOTPGroup>
                      {Array.from({ length: 6 }, (_, i) => (
                        <InputOTPSlot key={i} index={i} />
                      ))}
                    </InputOTPGroup>
                  </InputOTP>
                  <Button className='w-full' disabled={code.length !== 6 || submitting} onClick={onVerify}>
                    {submitting ? '验证中…' : '验证并登录'}
                  </Button>
                </>
              )}
              {mfaMethods.includes('passkey') && (
                <Button
                  variant={mfaMethods.includes('totp') ? 'outline' : 'default'}
                  className='w-full'
                  disabled={submitting}
                  onClick={onPasskey}
                >
                  使用 Passkey 登录
                </Button>
              )}
              <Button
                variant='ghost'
                size='sm'
                onClick={() => {
                  setMfaTicket(null)
                  setMfaMethods([])
                  setCode('')
                }}
              >
                返回重新输入账号
              </Button>
            </div>
          ) : (
            <form onSubmit={onSubmit} className='flex flex-col gap-4' noValidate>
              <div className='flex flex-col gap-2'>
                <Label htmlFor='username'>用户名</Label>
                <Input
                  id='username'
                  autoComplete='username'
                  placeholder='请输入用户名'
                  aria-invalid={!!form.formState.errors.username}
                  {...form.register('username')}
                />
                {form.formState.errors.username && (
                  <p className='text-sm text-destructive'>
                    {form.formState.errors.username.message}
                  </p>
                )}
              </div>
              <div className='flex flex-col gap-2'>
                <Label htmlFor='password'>密码</Label>
                <PasswordInput
                  id='password'
                  autoComplete='current-password'
                  placeholder='请输入密码'
                  aria-invalid={!!form.formState.errors.password}
                  {...form.register('password')}
                />
                {form.formState.errors.password && (
                  <p className='text-sm text-destructive'>
                    {form.formState.errors.password.message}
                  </p>
                )}
              </div>
              <Button type='submit' className='w-full' disabled={submitting}>
                {submitting ? '登录中…' : '登录'}
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </main>
  )
}
