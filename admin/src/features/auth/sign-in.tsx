import { useEffect, useRef, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import {
  ArrowLeft,
  Fingerprint,
  KeyRound,
  Loader2,
  ScrollText,
  ShieldCheck,
} from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { BRAND } from '@/config/navigation'
import { errorMessage } from '@/lib/http'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
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

/** 后台登录页要求的提示语，docs/06 开头的布局说明里写死了这句。 */
const AUDIT_NOTICE = '仅限授权人员 · 所有操作记入审计日志'

/** 左侧品牌面板的能力说明，对应 docs/10 第 5 节与 docs/06 第 34 节已实现的机制。 */
const HIGHLIGHTS = [
  {
    icon: ShieldCheck,
    title: '私有域名 + HttpOnly 会话',
    desc: '会话凭据不落前端存储，写操作校验 CSRF 令牌。',
  },
  {
    icon: Fingerprint,
    title: '二次验证',
    desc: '支持 TOTP 验证码与 Passkey，高敏操作需重新验证。',
  },
  {
    icon: ScrollText,
    title: '全量审计',
    desc: '每次写操作留存操作人、前后差异与请求来源。',
  },
]

export function SignIn() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { redirect } = useSearch({ from: '/(auth)/login' })
  const setSession = useAuthStore((s) => s.setSession)
  const [mfaTicket, setMfaTicket] = useState<string | null>(null)
  const [mfaMethods, setMfaMethods] = useState<Array<'totp' | 'passkey'>>([])
  const [code, setCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const otpRef = useRef<HTMLInputElement>(null)

  // 进入第二步时把焦点交给验证码输入框（多步流程的焦点管理，不用 autoFocus）
  useEffect(() => {
    if (mfaTicket) otpRef.current?.focus()
  }, [mfaTicket])

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

  const BrandIcon = BRAND.icon
  const errors = form.formState.errors

  return (
    <main className='grid min-h-svh lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]'>
      {/* 左：品牌面板。小屏隐藏，表单上方有紧凑版品牌行 */}
      <aside className='login-aside relative hidden flex-col justify-between overflow-hidden p-10 text-white lg:flex xl:p-14'>
        <div aria-hidden className='login-aside-grid absolute inset-0' />
        <div
          aria-hidden
          className='login-aside-orb absolute -top-24 -left-20 size-104 rounded-full'
        />
        <div
          aria-hidden
          className='login-aside-orb login-aside-orb-alt absolute -right-28 -bottom-24 size-88 rounded-full'
        />

        <div className='relative flex items-center gap-3'>
          <span className='grid size-10 place-items-center rounded-lg bg-white/12 ring-1 ring-white/20'>
            <BrandIcon className='size-5' />
          </span>
          <span className='text-base font-semibold tracking-wide'>{BRAND.name}</span>
        </div>

        <div className='relative max-w-md'>
          <h2 className='text-3xl leading-tight font-semibold text-balance xl:text-4xl'>
            内容、运营与平台配置的统一工作台
          </h2>
          <p className='mt-4 text-sm leading-relaxed text-white/65'>
            剧集与分集、前台页面与区块、评论与举报、存储线路与系统配置，都在这里维护。
          </p>

          <ul className='mt-10 flex flex-col gap-6'>
            {HIGHLIGHTS.map(({ icon: Icon, title, desc }) => (
              <li key={title} className='flex gap-4'>
                <span className='mt-0.5 grid size-9 shrink-0 place-items-center rounded-lg bg-white/10 ring-1 ring-white/15'>
                  <Icon className='size-4.5' />
                </span>
                <div>
                  <p className='text-sm font-medium'>{title}</p>
                  <p className='mt-1 text-xs leading-relaxed text-white/55'>{desc}</p>
                </div>
              </li>
            ))}
          </ul>
        </div>

        <p className='relative text-xs text-white/45'>{AUDIT_NOTICE}</p>
      </aside>

      {/* 右：登录表单 */}
      <div className='flex items-center justify-center px-6 py-10 sm:px-10'>
        <div className='w-full max-w-sm'>
          <div className='mb-10 flex items-center gap-3 lg:hidden'>
            <span className='grid size-10 place-items-center rounded-lg bg-primary text-primary-foreground'>
              <BrandIcon className='size-5' />
            </span>
            <span className='text-base font-semibold'>{BRAND.name}</span>
          </div>

          {mfaTicket ? (
            <section aria-labelledby='mfa-heading' className='flex flex-col gap-6'>
              <header className='flex flex-col gap-2'>
                <h1 id='mfa-heading' className='text-2xl font-semibold tracking-tight'>
                  二次验证
                </h1>
                <p className='text-sm text-muted-foreground'>
                  账号已验证，请再完成一次身份确认。
                </p>
              </header>

              {mfaMethods.includes('totp') && (
                <div className='flex flex-col gap-3'>
                  <Label htmlFor='mfa-code'>验证码</Label>
                  <InputOTP
                    id='mfa-code'
                    ref={otpRef}
                    maxLength={6}
                    value={code}
                    onChange={setCode}
                    onComplete={onVerify}
                    aria-describedby='mfa-code-hint'
                  >
                    <InputOTPGroup className='gap-2'>
                      {Array.from({ length: 6 }, (_, i) => (
                        <InputOTPSlot
                          key={i}
                          index={i}
                          className='size-11 rounded-md border text-base font-medium'
                        />
                      ))}
                    </InputOTPGroup>
                  </InputOTP>
                  <p id='mfa-code-hint' className='text-xs text-muted-foreground'>
                    请输入身份验证应用中的 6 位验证码，输满 6 位会自动提交。
                  </p>
                  <Button
                    className='h-11 w-full'
                    disabled={code.length !== 6 || submitting}
                    onClick={onVerify}
                  >
                    {submitting && <Loader2 className='animate-spin' />}
                    {submitting ? '验证中…' : '验证并登录'}
                  </Button>
                </div>
              )}

              {mfaMethods.includes('passkey') && (
                <div className='flex flex-col gap-3'>
                  {mfaMethods.includes('totp') && (
                    <div className='flex items-center gap-3 text-xs text-muted-foreground'>
                      <span className='h-px flex-1 bg-border' />
                      或
                      <span className='h-px flex-1 bg-border' />
                    </div>
                  )}
                  <Button
                    variant={mfaMethods.includes('totp') ? 'outline' : 'default'}
                    className='h-11 w-full'
                    disabled={submitting}
                    onClick={onPasskey}
                  >
                    <KeyRound />
                    使用 Passkey 登录
                  </Button>
                </div>
              )}

              <Button
                variant='ghost'
                className='h-10 self-start px-2 text-muted-foreground hover:text-foreground'
                onClick={() => {
                  setMfaTicket(null)
                  setMfaMethods([])
                  setCode('')
                }}
              >
                <ArrowLeft />
                返回重新输入账号
              </Button>
            </section>
          ) : (
            <>
              <header className='flex flex-col gap-2'>
                <h1 className='text-2xl font-semibold tracking-tight'>登录</h1>
                <p className='text-sm text-muted-foreground'>
                  请使用分配给你的后台账号登录。
                </p>
              </header>

              <form onSubmit={onSubmit} className='mt-8 flex flex-col gap-5' noValidate>
                <div className='flex flex-col gap-2'>
                  <Label htmlFor='username'>用户名</Label>
                  <Input
                    id='username'
                    className='h-11'
                    autoComplete='username'
                    autoCapitalize='none'
                    spellCheck={false}
                    placeholder='请输入用户名'
                    aria-invalid={!!errors.username}
                    aria-describedby={errors.username ? 'username-error' : undefined}
                    {...form.register('username')}
                  />
                  {errors.username && (
                    <p id='username-error' role='alert' className='text-sm text-destructive'>
                      {errors.username.message}
                    </p>
                  )}
                </div>

                <div className='flex flex-col gap-2'>
                  <Label htmlFor='password'>密码</Label>
                  <PasswordInput
                    id='password'
                    className='[&_input]:h-11'
                    autoComplete='current-password'
                    placeholder='请输入密码'
                    aria-invalid={!!errors.password}
                    aria-describedby={errors.password ? 'password-error' : undefined}
                    {...form.register('password')}
                  />
                  {errors.password && (
                    <p id='password-error' role='alert' className='text-sm text-destructive'>
                      {errors.password.message}
                    </p>
                  )}
                </div>

                <Button type='submit' className='mt-1 h-11 w-full' disabled={submitting}>
                  {submitting && <Loader2 className='animate-spin' />}
                  {submitting ? '登录中…' : '登录'}
                </Button>
              </form>

              <div className='mt-8 flex gap-3 rounded-md border bg-muted/40 p-4'>
                <ShieldCheck className='mt-0.5 size-4 shrink-0 text-muted-foreground' />
                <div className='text-xs leading-relaxed text-muted-foreground'>
                  <p className='font-medium text-foreground'>{AUDIT_NOTICE}</p>
                  <p className='mt-1'>
                    忘记密码或需要开通账号，请联系超级管理员；连续输错会暂时锁定登录。
                  </p>
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </main>
  )
}
