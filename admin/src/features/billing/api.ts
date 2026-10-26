import { queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

export type VIPPlanCode = 'day' | 'week' | 'month' | 'quarter' | 'year'
export type PaymentChannel = 'alipay' | 'wechat' | 'usdt' | 'iap_apple' | 'iap_google'
export type VIPClient = 'web_pc' | 'web_mobile' | 'app_official' | 'app_store'
export type VIPPlanStatus = 'on' | 'off'

export type AdminVIPPlan = {
  id: number
  code: VIPPlanCode
  duration_days: number
  price_fen: number
  list_price_fen: number
  price_micro_usdt: number
  badge_key: string
  recommended: boolean
  channels: PaymentChannel[]
  clients: VIPClient[]
  sort: number
  status: VIPPlanStatus
  updated_at: string
}

export type AdminVIPPlanInput = Omit<AdminVIPPlan, 'id' | 'code' | 'updated_at'>

export const billingKeys = {
  all: ['billing'] as const,
  vipPlans: ['billing', 'vip-plans'] as const,
}

export const vipPlansQuery = queryOptions({
  queryKey: billingKeys.vipPlans,
  queryFn: () => http.get<{ list: AdminVIPPlan[] }>('/vip/plans').then((data) => data.list),
})

export const billingApi = {
  updateVIPPlan: (id: number, input: AdminVIPPlanInput) => http.patch<AdminVIPPlan>(`/vip/plans/${id}`, input),
}

export const PLAN_CODE_LABELS: Record<VIPPlanCode, string> = {
  day: '天卡',
  week: '周卡',
  month: '月卡',
  quarter: '季卡',
  year: '年卡',
}

export const CHANNEL_LABELS: Record<PaymentChannel, string> = {
  alipay: '支付宝',
  wechat: '微信支付',
  usdt: 'USDT',
  iap_apple: 'Apple 内购',
  iap_google: 'Google Play 结算',
}

export const CLIENT_LABELS: Record<VIPClient, string> = {
  web_pc: 'Web 电脑',
  web_mobile: 'Web 手机',
  app_official: 'APP 官网包',
  app_store: 'APP 商店包',
}
