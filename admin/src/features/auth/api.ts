import {
  startAuthentication,
  startRegistration,
  type PublicKeyCredentialCreationOptionsJSON,
  type PublicKeyCredentialRequestOptionsJSON,
} from '@simplewebauthn/browser'
import { queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { AdminUser } from '@/stores/auth-store'

export type MeResult = { user: AdminUser; csrf_token: string; permissions: string[] }

/** 一把已注册的 Passkey（GET /api/admin/mfa/passkeys）。 */
export type PasskeyInfo = { id: number; name: string; created_at: string; last_used_at: string | null }

/** POST /api/admin/login 的结果：需要二次验证时返回 mfa_ticket（S2-6 起），否则直接返回会话。 */
export type LoginResult =
  | { mfa_required: true; mfa_ticket: string; mfa_methods: Array<'totp' | 'passkey'> }
  | ({ mfa_required: false } & MeResult)

/**
 * 侧栏菜单的一项（GET /api/admin/me/menus，已按当前管理员的权限过滤）。
 * 一级菜单可以是目录、页面或外链；目录的 children 是二级菜单，其余为空数组。
 */
export type NavNode = {
  code: string
  kind: 'group' | 'page' | 'link'
  title: string
  path: string
  icon: string
  children: NavNode[]
}

/** POST /api/admin/mfa/totp/setup 的结果：待确认的 TOTP 种子（10 分钟内用验证码确认才生效）。 */
export type TotpSetup = { secret: string; otpauth_url: string }

export const authApi = {
  login: (username: string, password: string) =>
    http.post<LoginResult>('/login', { username, password }),
  verifyTotp: (mfaTicket: string, code: string) =>
    http.post<MeResult>('/login/mfa', { mfa_ticket: mfaTicket, method: 'totp', code }),
  me: () => http.get<MeResult>('/me'),
  logout: () => http.post<void>('/logout'),
  // 自助绑定二次验证（S2-6）：setup 生成待确认种子，verify 用一次性验证码确认后启用。
  beginTotpSetup: () => http.post<TotpSetup>('/mfa/totp/setup'),
  confirmTotpSetup: (code: string) => http.post<void>('/mfa/totp/verify', { code }),
  // 高敏操作重新验证（S2-6）：通过后本会话 5 分钟内可执行高敏写操作。
  reauth: (code: string) => http.post<void>('/reauth', { method: 'totp', code }),

  // ---- Passkey / WebAuthn（S2-6）----
  listPasskeys: () => http.get<PasskeyInfo[]>('/mfa/passkeys'),
  deletePasskey: (id: number) => http.delete<void>(`/mfa/passkeys/${id}`),
  // 自助注册：begin 取创建选项 → 浏览器调认证器 → finish 保存
  registerPasskey: async (name: string) => {
    const options = await http.post<PublicKeyCredentialCreationOptionsJSON>('/mfa/passkey/register/begin')
    const response = await startRegistration({ optionsJSON: options })
    await http.post<void>('/mfa/passkey/register/finish', { name, response })
  },
  // 登录第二步用 Passkey：begin 取断言选项 → 浏览器签名 → /login/mfa 换会话
  loginPasskey: async (mfaTicket: string) => {
    const options = await http.post<PublicKeyCredentialRequestOptionsJSON>('/login/passkey/begin', {
      mfa_ticket: mfaTicket,
    })
    const response = await startAuthentication({ optionsJSON: options })
    return http.post<MeResult>('/login/mfa', { mfa_ticket: mfaTicket, method: 'passkey', response })
  },
  // 高敏重验用 Passkey
  reauthPasskey: async () => {
    const options = await http.post<PublicKeyCredentialRequestOptionsJSON>('/reauth/passkey/begin')
    const response = await startAuthentication({ optionsJSON: options })
    await http.post<void>('/reauth', { method: 'passkey', response })
  },
}

/** 当前会话。staleTime 5 分钟，路由守卫与页头共用。 */
export const meQuery = queryOptions({
  queryKey: ['auth', 'me'],
  queryFn: () => authApi.me(),
  staleTime: 5 * 60 * 1000,
  retry: false,
})

/** 当前管理员的侧栏菜单。修改后台菜单或权限后失效重取。 */
export const myMenusQuery = queryOptions({
  queryKey: ['auth', 'menus'],
  queryFn: () => http.get<NavNode[]>('/me/menus'),
  staleTime: 5 * 60 * 1000,
})
