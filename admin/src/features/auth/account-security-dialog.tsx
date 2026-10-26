import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, KeyRound, Plus, ShieldCheck, Trash2 } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@/components/ui/input-otp'
import { Label } from '@/components/ui/label'
import { authApi, meQuery, type TotpSetup } from './api'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 强制开启模式（S2-6 切片 7）：超管未开启二次验证时不可关闭，直到绑定任一因子。 */
  mandatory?: boolean
}

const passkeysKey = ['auth', 'passkeys'] as const

/** 刷新会话：Passkey / TOTP 变化会影响 user.mfa_enabled。 */
function useRefreshSession() {
  const queryClient = useQueryClient()
  const setSession = useAuthStore((s) => s.setSession)
  return async () => {
    const session = await authApi.me()
    setSession(session.user, session.csrf_token, session.permissions)
    queryClient.setQueryData(meQuery.queryKey, session)
  }
}

/** Passkey 管理：列出、添加（浏览器调认证器）、删除。 */
function PasskeySection() {
  const queryClient = useQueryClient()
  const refreshSession = useRefreshSession()
  const list = useQuery({ queryKey: passkeysKey, queryFn: () => authApi.listPasskeys() })
  const [name, setName] = useState('')

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: passkeysKey })
    void refreshSession()
  }

  const add = useMutation({
    mutationFn: () => authApi.registerPasskey(name.trim() || 'Passkey'),
    onSuccess: () => {
      setName('')
      toast.success('已添加 Passkey')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const del = useMutation({
    mutationFn: (id: number) => authApi.deletePasskey(id),
    onSuccess: () => {
      toast.success('已删除 Passkey')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  const items = list.data ?? []
  return (
    <section className='flex flex-col gap-3'>
      <div className='flex items-center gap-2 text-sm font-medium'>
        <KeyRound className='size-4' /> Passkey（指纹 / 面容 / 安全密钥）
      </div>
      {items.length > 0 ? (
        <ul className='flex flex-col gap-2'>
          {items.map((pk) => (
            <li key={pk.id} className='flex items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm'>
              <span className='truncate'>{pk.name || '未命名'}</span>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                className='h-8 w-8 shrink-0 text-destructive'
                aria-label={`删除 ${pk.name || '未命名'}`}
                disabled={del.isPending}
                onClick={() => del.mutate(pk.id)}
              >
                <Trash2 className='size-4' />
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <p className='text-sm text-muted-foreground'>还没有 Passkey。添加后登录时可用设备的指纹 / 面容 / 安全密钥验证。</p>
      )}
      <div className='flex items-center gap-2'>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder='给这把 Passkey 起个名字（如 办公室电脑）'
          maxLength={64}
          className='h-10'
        />
        <Button type='button' className='h-10 shrink-0' disabled={add.isPending} onClick={() => add.mutate()}>
          <Plus className='size-4' /> {add.isPending ? '添加中…' : '添加'}
        </Button>
      </div>
    </section>
  )
}

/**
 * 账号安全：当前管理员自助绑定二次验证（TOTP，S2-6）。
 * 从页头用户菜单打开。流程：开始绑定 → 用认证器扫码或手动输入密钥 → 输入 6 位验证码确认启用。
 */
export function AccountSecurityDialog({ open, onOpenChange, mandatory = false }: Props) {
  const queryClient = useQueryClient()
  const user = useAuthStore((s) => s.user)
  const setSession = useAuthStore((s) => s.setSession)
  const mfaEnabled = user?.mfa_enabled ?? false

  const [setup, setSetup] = useState<TotpSetup | null>(null)
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)

  const reset = () => {
    setSetup(null)
    setCode('')
    setBusy(false)
  }

  const close = (next: boolean) => {
    if (mandatory && !next) return // 强制开启模式：绑定成功前不可关闭
    if (!next) reset()
    onOpenChange(next)
  }

  const begin = async () => {
    setBusy(true)
    try {
      setSetup(await authApi.beginTotpSetup())
      setCode('')
    } catch (error) {
      toast.error(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  const confirm = async () => {
    if (code.length !== 6 || busy) return
    setBusy(true)
    try {
      await authApi.confirmTotpSetup(code)
      // 刷新会话：user.mfa_enabled 变为 true
      const session = await authApi.me()
      setSession(session.user, session.csrf_token, session.permissions)
      queryClient.setQueryData(meQuery.queryKey, session)
      toast.success('已开启二次验证')
      close(false)
    } catch (error) {
      setCode('')
      toast.error(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  const copySecret = async () => {
    if (!setup) return
    try {
      await navigator.clipboard.writeText(setup.secret)
      toast.success('密钥已复制')
    } catch {
      toast.error('复制失败，请手动选择密钥文本')
    }
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className='sm:max-w-md' showCloseButton={!mandatory}>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <ShieldCheck className='size-5' /> 账号安全
          </DialogTitle>
          <DialogDescription>
            {mandatory
              ? '超级管理员必须开启二次验证后才能使用后台。绑定认证器 App 或 Passkey 后自动继续。'
              : '用身份验证器 App（如 Google Authenticator、1Password）或 Passkey 为后台账号开启二次验证。'}
          </DialogDescription>
        </DialogHeader>

        {setup ? (
          <div className='flex flex-col gap-4'>
            <div className='flex flex-col items-center gap-3'>
              <div className='rounded-md border bg-white p-3'>
                <QRCodeSVG value={setup.otpauth_url} size={168} />
              </div>
              <p className='text-center text-sm text-muted-foreground'>
                用认证器扫描二维码，或手动输入下面的密钥
              </p>
              <div className='flex w-full items-center gap-2'>
                <code className='flex-1 rounded-md border bg-muted px-3 py-2 text-center font-mono text-sm tracking-widest break-all'>
                  {setup.secret}
                </code>
                <Button
                  type='button'
                  variant='outline'
                  size='icon'
                  className='h-10 w-10 shrink-0'
                  aria-label='复制密钥'
                  onClick={copySecret}
                >
                  <Copy className='size-4' />
                </Button>
              </div>
            </div>
            <div className='flex flex-col items-center gap-2'>
              <Label htmlFor='totp-code'>输入认证器显示的 6 位验证码</Label>
              <InputOTP
                id='totp-code'
                maxLength={6}
                value={code}
                onChange={setCode}
                onComplete={confirm}
                aria-label='二次验证码'
              >
                <InputOTPGroup>
                  {Array.from({ length: 6 }, (_, i) => (
                    <InputOTPSlot key={i} index={i} />
                  ))}
                </InputOTPGroup>
              </InputOTP>
            </div>
            <DialogFooter>
              <Button type='button' variant='ghost' onClick={reset} disabled={busy}>
                重新开始
              </Button>
              <Button type='button' onClick={confirm} disabled={code.length !== 6 || busy}>
                {busy ? '验证中…' : '确认开启'}
              </Button>
            </DialogFooter>
          </div>
        ) : (
          <div className='flex flex-col gap-5'>
            <div className='flex items-center gap-3 rounded-md border p-3'>
              <span
                className={
                  mfaEnabled
                    ? 'inline-flex size-2.5 shrink-0 rounded-full bg-emerald-500'
                    : 'inline-flex size-2.5 shrink-0 rounded-full bg-muted-foreground/40'
                }
              />
              <p className='text-sm'>
                {mfaEnabled ? '二次验证已开启' : '二次验证未开启'}
                <span className='text-muted-foreground'> · 登录时在账号密码之外再验证一次</span>
              </p>
            </div>

            <section className='flex flex-col gap-2'>
              <div className='flex items-center gap-2 text-sm font-medium'>
                <ShieldCheck className='size-4' /> 认证器 App（动态验证码）
              </div>
              <div className='flex items-center justify-between gap-3 rounded-md border p-3'>
                <p className='text-sm text-muted-foreground'>
                  用 Google Authenticator、1Password 等生成 6 位动态验证码。重新绑定会替换现有验证器。
                </p>
                <Button type='button' size='sm' className='shrink-0' onClick={begin} disabled={busy}>
                  {busy ? '准备中…' : '绑定'}
                </Button>
              </div>
            </section>

            <PasskeySection />
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
