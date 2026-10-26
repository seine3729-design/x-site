import 'server-only'
import { callService, tryService } from '@/lib/server/api'
import { getSessionToken } from '@/lib/server/session'
import { pageOf } from './page'
import type {
  FbAnswer,
  FbFeedback,
  FbFeedbackDetail,
  FbLeaderboardEntry,
  FbPost,
  FbProjectView,
  FbTopic,
  Page,
} from './types'

/**
 * 前台的读接口集合。
 *
 * 全部带当前会话令牌：列表的可见性过滤在后端 SQL 里做（公开 / 仅成员 / 私密），
 * 所以同一个地址对不同人返回不同内容——这也是这些响应一律 no-store 的原因，
 * 共享缓存会串号（feedback/docs/06 第 10 节）。
 *
 * 需要可缓存的公开视角数据（首页用）见 ./public-queries.ts，
 * 那边不带身份，也因此不能用来取任何带个人数据的接口。
 */

type ListParams = Record<string, string | number | boolean | undefined>

async function get<T>(path: string, query?: ListParams): Promise<T> {
  return callService<T>(path, { token: await getSessionToken(), query })
}

async function getOrNull<T>(path: string, query?: ListParams): Promise<T | null> {
  return tryService<T>(path, { token: await getSessionToken(), query })
}

// ---------- 项目 ----------

export async function listProjects(params: { page?: number; page_size?: number } = {}) {
  return pageOf(await get<Page<FbProjectView>>('/api/v1/feedback/projects', params))
}

export async function getProject(slug: string) {
  return getOrNull<FbProjectView>(`/api/v1/feedback/projects/${encodeURIComponent(slug)}`)
}

export async function listMyProjects() {
  const data = await get<{ list: FbProjectView[] }>('/api/v1/feedback/me/projects')
  return data.list ?? []
}

// ---------- 动态 ----------

export async function listPosts(params: {
  project_id?: number
  page?: number
  page_size?: number
}) {
  return pageOf(await get<Page<FbPost>>('/api/v1/feedback/posts', params))
}

export async function getPost(id: number) {
  return getOrNull<FbPost>(`/api/v1/feedback/posts/${id}`)
}

// ---------- 反馈 ----------

export type FeedbackQuery = {
  project_id?: number
  status?: string
  kind?: string
  q?: string
  sort?: string
  page?: number
  page_size?: number
}

export async function listFeedback(params: FeedbackQuery = {}) {
  return pageOf(await get<Page<FbFeedback>>('/api/v1/feedback/issues', params))
}

export async function getFeedback(slug: string, number: number) {
  return getOrNull<FbFeedbackDetail>(
    `/api/v1/feedback/issues/${encodeURIComponent(slug)}/${number}`,
  )
}

export async function listMyFeedback(params: { page?: number; page_size?: number } = {}) {
  return pageOf(await get<Page<FbFeedback>>('/api/v1/feedback/me/issues', params))
}

// ---------- 议题 ----------

export async function listTopics(params: {
  project_id?: number
  status?: 'open' | 'closed'
  page?: number
  page_size?: number
} = {}) {
  return pageOf(await get<Page<FbTopic>>('/api/v1/feedback/topics', params))
}

export async function getTopic(id: number) {
  return getOrNull<FbTopic>(`/api/v1/feedback/topics/${id}`)
}

export async function listAnswers(topicId: number, params: { page?: number; page_size?: number } = {}) {
  return pageOf(await get<Page<FbAnswer>>(`/api/v1/feedback/topics/${topicId}/answers`, params))
}

// ---------- 贡献榜 ----------

export async function listLeaderboard(limit = 20) {
  const data = await get<{ list: FbLeaderboardEntry[] }>('/api/v1/feedback/leaderboard', { limit })
  return data.list ?? []
}
