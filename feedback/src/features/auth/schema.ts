import { z } from 'zod'

/**
 * 账号表单的校验规则。
 *
 * 和后端 `domain/feedback/validate.go` 保持一致：规则不一致会让用户在前端通过、
 * 到后端被拒，拿到两套说法。后端仍然是权威，这里只是提前告知。
 */

const handle = z
  .string()
  .trim()
  .regex(/^[A-Za-z0-9_-]{3,32}$/, '用户名只能用 3–32 位字母、数字、下划线或连字符')
  .refine((v) => !/^\d+$/.test(v), '用户名不能全是数字')

const password = z
  .string()
  .min(12, '密码至少 12 位')
  .max(128, '密码最多 128 位')
  .refine((v) => v.trim() !== '', '密码不能只有空格')

export const loginSchema = z.object({
  handle: z.string().trim().min(1, '请填写用户名'),
  password: z.string().min(1, '请填写密码'),
})

export const registerSchema = z.object({
  handle,
  password,
  // 字段顺序就是表单的渲染顺序：用户名、密码、昵称、邀请码。
  // 邀请码放最后——它通常是从别处复制粘贴过来的，而前三项要现想。
  nickname: z.string().trim().max(40, '昵称最多 40 个字'),
  invite_code: z.string().trim().min(1, '请填写邀请码'),
})

// 没有 recoverSchema：恢复码与「忘记密码」已删除（adr/0011）。
// 改密走 features/settings/actions.ts，必须提供当前密码。

/** 表单返回给页面的状态。fieldErrors 的键是表单字段名。 */
export type AuthState = {
  error?: string
  fieldErrors?: Record<string, string>
}

/** 把 zod 的错误摊平成 { 字段: 第一条错误 }。 */
export function fieldErrorsOf(error: z.ZodError): Record<string, string> {
  const out: Record<string, string> = {}
  for (const issue of error.issues) {
    const key = String(issue.path[0] ?? '')
    if (key && !out[key]) out[key] = issue.message
  }
  return out
}
