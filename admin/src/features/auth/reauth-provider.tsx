import { useCallback, useEffect, useRef, useState } from 'react'
import { KeyRound, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage, setReauthHandler } from '@/lib/http'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@/components/ui/input-otp'
import { Label } from '@/components/ui/label'
import { authApi } from './api'

/**
 * 高敏操作重新验证（S2-6）。注册到 http 层：标 sensitive 的写请求收到 412 REAUTH_REQUIRED 时弹窗，
 * 让当前管理员输入认证器验证码重新验证，通过后 http 层自动重试原请求（对各页面透明）。
 * 挂在认证后的布局里一次即可。
 */
export function ReauthProvider() {
  const [open, setOpen] = useState(false)
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  // 把「弹窗结果」交回给发起 412 的请求：resolve(true) 重试、resolve(false) 放弃
  const resolveRef = useRef<((ok: boolean) => void) | null>(null)

  const settle = useCallback((ok: boolean) => {
    resolveRef.current?.(ok)
    resolveRef.current = null
    setOpen(false)
    setCode('')
    setBusy(false)
  }, [])

  useEffect(() => {
    setReauthHandler(
      () =>
        new Promise<boolean>((resolve) => {
          resolveRef.current = resolve
          setCode('')
          setBusy(false)
          setOpen(true)
        })
    )
    return () => setReauthHandler(null)
  }, [])

  const succeed = () => {
    resolveRef.current?.(true)
    resolveRef.current = null
    setOpen(false)
    setCode('')
  }

  const verify = async () => {
    if (code.length !== 6 || busy) return
    setBusy(true)
    try {
      await authApi.reauth(code)
      succeed()
    } catch (error) {
      setCode('')
      toast.error(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  const verifyPasskey = async () => {
    if (busy) return
    setBusy(true)
    try {
      await authApi.reauthPasskey()
      succeed()
    } catch (error) {
      toast.error(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) settle(false) // 关闭或取消＝放弃本次操作
      }}
    >
      <DialogContent className='sm:max-w-sm'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <ShieldCheck className='size-5' /> 重新验证身份
          </DialogTitle>
          <DialogDescription>这是高敏操作，请输入认证器当前显示的 6 位验证码后继续。</DialogDescription>
        </DialogHeader>
        <div className='flex flex-col items-center gap-3 py-2'>
          <Label htmlFor='reauth-code' className='sr-only'>
            验证码
          </Label>
          <InputOTP
            id='reauth-code'
            maxLength={6}
            value={code}
            onChange={setCode}
            onComplete={verify}
            aria-label='二次验证码'
          >
            <InputOTPGroup>
              {Array.from({ length: 6 }, (_, i) => (
                <InputOTPSlot key={i} index={i} />
              ))}
            </InputOTPGroup>
          </InputOTP>
          <Button type='button' variant='outline' className='w-full' disabled={busy} onClick={verifyPasskey}>
            <KeyRound className='size-4' /> 用 Passkey 验证
          </Button>
        </div>
        <DialogFooter>
          <Button type='button' variant='ghost' onClick={() => settle(false)} disabled={busy}>
            取消
          </Button>
          <Button type='button' onClick={verify} disabled={code.length !== 6 || busy}>
            {busy ? '验证中…' : '验证并继续'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
