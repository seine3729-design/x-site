import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { Page } from '@/features/content/api'

/**
 * 后台用户管理（docs/06 第 26 节，docs/07「用户」；契约 service/api/openapi.yaml 0.59.0）。
 *
 * 不提供重设用户密码（docs/adr/0001）：账号只有用户名 + 密码，没有找回渠道，后台也不开后门。
 * 删除 / 回收站本期未接入；会员、等级、金币、邀请、风控等详情字段要等 S3-10 与 M6。
 */

export type UserStatus = 'normal' | 'muted' | 'banned'

export const USER_STATUSES: UserStatus[] = ['normal', 'muted', 'banned']

export const USER_STATUS_LABELS: Record<UserStatus, string> = {
  normal: '正常',
  muted: '禁言',
  banned: '封禁',
}

/** 每种状态的实际影响，用在下拉框说明与确认弹窗里。 */
export const USER_STATUS_HINTS: Record<UserStatus, string> = {
  normal: '可以登录、观看、发评论和弹幕。',
  muted: '可以登录和观看，发不出评论与弹幕。',
  banned: '立即下线全部设备，不能再登录，也发不出评论与弹幕。',
}

export type AdminUser = {
  id: number
  username: string
  nickname: string
  avatar: string
  bio: string
  status: UserStatus
  locale: string
  /** 为空表示未做过验证 */
  verified_at: string | null
  created_at: string
}

/** 资料审核状态（昵称 / 简介 / 头像命中先审词时为 pending）。 */
export type ProfileReviewStatus = 'ok' | 'pending' | 'rejected'

export const PROFILE_REVIEW_LABELS: Record<ProfileReviewStatus, string> = {
  ok: '正常',
  pending: '待审核',
  rejected: '已拒绝',
}

export type AdminUserStats = {
  comment_count: number
  danmaku_count: number
  report_sent: number
  report_against: number
  follow_count: number
  history_count: number
  session_count: number
}

export type AdminUserDetail = AdminUser & {
  nickname_status: ProfileReviewStatus
  bio_status: ProfileReviewStatus
  avatar_status: ProfileReviewStatus
  history_paused: boolean
  stats: AdminUserStats
}

export type Guest = {
  id: string
  first_seen: string
  source_channel: string
  platform: string
  attest_level: string
  merged_user_id: number | null
  merged_username: string
}

export const GUEST_PLATFORM_LABELS: Record<string, string> = { web: '网页', ios: 'iOS', android: 'Android' }
export const ATTEST_LEVEL_LABELS: Record<string, string> = { none: '未校验', basic: '基础', strong: '强校验' }

export type UserListParams = {
  q?: string
  status?: UserStatus
  page: number
  page_size: number
}

export type GuestListParams = {
  q?: string
  page: number
  page_size: number
}

export const userKeys = {
  all: ['community', 'users'] as const,
  list: (p: UserListParams) => ['community', 'users', 'list', p] as const,
  detail: (id: number) => ['community', 'users', 'detail', id] as const,
  guests: (p: GuestListParams) => ['community', 'guests', 'list', p] as const,
}

export const userListQuery = (p: UserListParams) =>
  queryOptions({
    queryKey: userKeys.list(p),
    queryFn: () => http.get<Page<AdminUser>>('/users', { query: p }),
    placeholderData: keepPreviousData,
  })

export const guestListQuery = (p: GuestListParams) =>
  queryOptions({
    queryKey: userKeys.guests(p),
    queryFn: () => http.get<Page<Guest>>('/guests', { query: p }),
    placeholderData: keepPreviousData,
  })

export const userDetailQuery = (id: number) =>
  queryOptions({
    queryKey: userKeys.detail(id),
    queryFn: () => http.get<AdminUserDetail>(`/users/${id}`),
  })

export const userApi = {
  setStatus: (id: number, body: { status: UserStatus }) => http.post<AdminUser>(`/users/${id}/status`, body),
}
