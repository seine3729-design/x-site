import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { Page } from '@/features/content/api'

/**
 * 后台「社区」目录的接口（docs/07 第 2 节，docs/06 第 25–27 节；契约 service/api/openapi.yaml：屏蔽词 0.44.0）。
 * S2-4 先做屏蔽词；评论 / 弹幕审核、举报、用户随 S2-5 起接入。
 */

// ---------- 屏蔽词 ----------

export type BlockAction = 'block' | 'mask' | 'review'

export const BLOCK_ACTIONS = ['block', 'mask', 'review'] as const

export const BLOCK_ACTION_LABELS: Record<BlockAction, string> = {
  block: '直接拦下',
  mask: '变成 ***',
  review: '先送审核',
}

/** 处理办法的生效范围说明（docs/06 第 27 节）。 */
export const BLOCK_ACTION_HINTS: Record<BlockAction, string> = {
  block: '评论 / 弹幕 / 昵称 / 简介命中后发不出；采集剧名命中不收录。',
  mask: '评论 / 弹幕 / 简介里把命中的词替换成 ***，照常显示。',
  review: '评论 / 弹幕 / 昵称 / 简介命中后进审核队列，先不显示。',
}

export type Blockword = {
  id: number
  /** 规范化后的词（全角转半角、小写、去零宽字符） */
  word: string
  group: string
  action: BlockAction
  created_at: string
}

export type BlockwordListParams = {
  q?: string
  group?: string
  action?: BlockAction
  page: number
  page_size: number
}

export const communityKeys = {
  all: ['community'] as const,
  blockwords: ['community', 'blockwords'] as const,
  blockwordList: (p: BlockwordListParams) => ['community', 'blockwords', 'list', p] as const,
  blockwordGroups: ['community', 'blockwords', 'groups'] as const,
}

export const blockwordListQuery = (p: BlockwordListParams) =>
  queryOptions({
    queryKey: communityKeys.blockwordList(p),
    queryFn: () => http.get<Page<Blockword>>('/blockwords', { query: p }),
    placeholderData: keepPreviousData,
  })

export const blockwordGroupsQuery = () =>
  queryOptions({
    queryKey: communityKeys.blockwordGroups,
    queryFn: () => http.get<{ groups: string[] }>('/blockwords/groups'),
  })

export const communityApi = {
  createBlockword: (body: { word: string; group: string; action: BlockAction }) => http.post<Blockword>('/blockwords', body),
  deleteBlockword: (id: number) => http.delete<void>(`/blockwords/${id}`),
}

// ---------- 表情（docs/06「表情/表情包管理」，契约 0.53.0） ----------

export type Emoji = {
  id: number
  pack_id: number
  /** 表情码，形如 [微笑]，全局唯一 */
  code: string
  image: string
  sort: number
  created_at: string
}

export type EmojiPack = {
  id: number
  name: string
  sort: number
  created_at: string
  emojis: Emoji[]
}

export const emojiKeys = {
  all: ['community', 'emojis'] as const,
}

export const emojiListQuery = () =>
  queryOptions({
    queryKey: emojiKeys.all,
    queryFn: () => http.get<{ packs: EmojiPack[] }>('/emojis'),
  })

export const emojiApi = {
  createPack: (body: { name: string; sort: number }) => http.post<EmojiPack>('/emojis/packs', body),
  updatePack: (id: number, body: { name: string; sort: number }) => http.patch<EmojiPack>(`/emojis/packs/${id}`, body),
  deletePack: (id: number) => http.delete<void>(`/emojis/packs/${id}`),
  createEmoji: (packId: number, body: { code: string; image: string; sort: number }) => http.post<Emoji>(`/emojis/packs/${packId}/emojis`, body),
  updateEmoji: (id: number, body: { code: string; image: string; sort: number }) => http.patch<Emoji>(`/emojis/items/${id}`, body),
  deleteEmoji: (id: number) => http.delete<void>(`/emojis/items/${id}`),
}

// ---------- 举报队列（docs/06 第 25 节，契约 0.55.0） ----------

export type ReportStatus = 'pending' | 'resolved' | 'rejected'
export type ReportTarget = 'comment' | 'danmaku' | 'drama' | 'comment_image'
export type ReportReason = 'attack' | 'vulgar' | 'illegal' | 'minor' | 'ad' | 'spoiler' | 'other'

export const REPORT_STATUSES: ReportStatus[] = ['pending', 'resolved', 'rejected']
export const REPORT_STATUS_LABELS: Record<ReportStatus, string> = { pending: '等待处理', resolved: '已处理', rejected: '已驳回' }

export const REPORT_TARGETS: ReportTarget[] = ['comment', 'danmaku', 'drama', 'comment_image']
export const REPORT_TARGET_LABELS: Record<ReportTarget, string> = { comment: '评论', danmaku: '弹幕', drama: '剧', comment_image: '评论图片' }

export const REPORT_REASONS: ReportReason[] = ['attack', 'vulgar', 'illegal', 'minor', 'ad', 'spoiler', 'other']
export const REPORT_REASON_LABELS: Record<ReportReason, string> = {
  attack: '人身攻击',
  vulgar: '低俗色情',
  illegal: '违法违规',
  minor: '涉未成年',
  ad: '广告引流',
  spoiler: '剧透',
  other: '其他',
}

export type Reporter = {
  is_guest: boolean
  user_id?: number
  nickname: string
  avatar: string
}

export type Report = {
  id: number
  target_type: ReportTarget
  target_id: number
  reason: ReportReason
  detail: string
  status: ReportStatus
  handle_note: string
  reporter: Reporter
  created_at: string
}

export type ReportListParams = {
  q?: string
  status?: ReportStatus
  target_type?: ReportTarget
  reason?: ReportReason
  page: number
  page_size: number
}

export const reportKeys = {
  all: ['community', 'reports'] as const,
  list: (p: ReportListParams) => ['community', 'reports', 'list', p] as const,
}

export const reportListQuery = (p: ReportListParams) =>
  queryOptions({
    queryKey: reportKeys.list(p),
    queryFn: () => http.get<Page<Report>>('/reports', { query: p }),
    placeholderData: keepPreviousData,
  })

export const reportApi = {
  handle: (id: number, body: { status: Exclude<ReportStatus, 'pending'>; note: string }) => http.post<Report>(`/reports/${id}/handle`, body),
}

// ---------- 评论 / 弹幕审核（docs/06 第 24 节，契约 0.58.0） ----------

export type ModerationKind = 'comment' | 'danmaku'
export type ModerationStatus = 'pending' | 'visible' | 'hidden'
export type ModerationAction = 'approve' | 'hide' | 'delete'

export const MODERATION_STATUSES: ModerationStatus[] = ['pending', 'visible', 'hidden']
export const MODERATION_STATUS_LABELS: Record<ModerationStatus, string> = { pending: '等待审核', visible: '已显示', hidden: '已隐藏' }

export type ModerationItem = {
  id: number
  kind: ModerationKind
  episode_id: number
  content: string
  status: ModerationStatus
  author: Reporter
  report_count: number
  hit_words: string[]
  created_at: string
  parent_id?: number
  like_count?: number
  time_ms?: number
  color?: string
}

export type ModerationListParams = {
  kind: ModerationKind
  status?: ModerationStatus
  reported?: boolean
  blockword?: boolean
  q?: string
  page: number
  page_size: number
}

export type CommentPolicy = { pre_moderation: boolean }

export const moderationKeys = {
  all: ['community', 'moderation'] as const,
  list: (p: ModerationListParams) => ['community', 'moderation', 'list', p] as const,
  policy: ['community', 'comment-policy'] as const,
}

export const moderationListQuery = (p: ModerationListParams) =>
  queryOptions({
    queryKey: moderationKeys.list(p),
    queryFn: () => http.get<Page<ModerationItem>>('/moderation', { query: p }),
    placeholderData: keepPreviousData,
  })

export const commentPolicyQuery = () =>
  queryOptions({
    queryKey: moderationKeys.policy,
    queryFn: () => http.get<CommentPolicy>('/comment-policy'),
  })

export const moderationApi = {
  act: (body: { kind: ModerationKind; action: ModerationAction; ids: number[] }) => http.post<{ affected: number }>('/moderation/actions', body),
  setPolicy: (body: CommentPolicy) => http.put<CommentPolicy>('/comment-policy', body),
}
