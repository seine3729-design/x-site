import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/**
 * 后台内容接口（docs/07 第 2 节「剧集 / 分集 / 标签 / 维度 / 专题」，契约 service/api/openapi.yaml：
 * 内容接口 0.7.0 起，0.9.0 补充了分集视频状态 media_status / video_object_id 的含义，0.11.0 删除了演员）。
 * 类型与契约中的 Admin* schema 逐字段对应；X0-5 接入生成的类型后替换。
 */

// ---------- 通用 ----------

/** 页码分页的一页。 */
export type Page<T> = { list: T[]; total: number; page: number; page_size: number }

/** 一次返回全部的列表。 */
export type List<T> = { list: T[]; total: number }

export type ActiveStatus = 'active' | 'inactive'

// ---------- 剧集 ----------

export type DramaStatus = 'draft' | 'published' | 'offline'
/** 列表显示用的状态：已上架但定时未到点为 scheduled。 */
export type DisplayStatus = DramaStatus | 'scheduled'
export type DramaSort = 'latest' | 'updated' | 'hot' | 'recommend'

export type Drama = {
  id: number
  title: string
  aliases: string[]
  slug: string
  summary: string
  cover_portrait: string
  cover_landscape: string
  badge: string
  score: number | null
  year: number | null
  status: DramaStatus
  display_status: DisplayStatus
  publish_at: string | null
  is_finished: boolean
  sort_weight: number
  view_base: number
  view_count: number
  hot_score: number
  latest_episode: number
  seo_title: string
  seo_desc: string
  source: string
  created_at: string
  updated_at: string
}

export type DramaTerm = {
  id: number
  taxonomy_id: number
  taxonomy_name: string
  multi_select: boolean
  name: string
  slug: string
  status: ActiveStatus
}

export type DramaDetail = Drama & { tags: Tag[]; terms: DramaTerm[] }

/** 新建剧集（一律为草稿）。 */
export type DramaCreateInput = { title: string; summary?: string }

/** 整体保存剧资料：未传的可选字段按默认值保存，所以每次提交完整表单。 */
export type DramaUpdateInput = {
  title: string
  aliases: string[]
  slug: string
  summary: string
  cover_portrait: string
  cover_landscape: string
  badge: string
  score: number | null
  year: number | null
  status: DramaStatus
  publish_at: string | null
  is_finished: boolean
  sort_weight: number
  view_base: number
  seo_title: string
  seo_desc: string
}

export type DramaListParams = {
  q?: string
  status?: DisplayStatus
  tag_id?: number
  term_id?: number
  sort?: DramaSort
  page?: number
  page_size?: number
}

export type BulkAction = 'publish' | 'offline' | 'draft' | 'add_tags' | 'add_to_topic' | 'set_terms'

export type BulkInput = {
  ids: number[]
  action: BulkAction
  tag_ids?: number[]
  topic_id?: number
  term_ids?: number[]
}

// ---------- 分集 ----------

export type Access = 'free' | 'login' | 'vip'
export type MediaStatus = 'pending' | 'transcoding' | 'ready' | 'failed'

export type Episode = {
  id: number
  drama_id: number
  number: number
  title: string
  cover: string
  duration_sec: number
  access: Access
  trial_sec: number
  media_status: MediaStatus
  video_object_id: number | null
  created_at: string
  updated_at: string
}

export type EpisodeInput = {
  number: number
  title: string
  cover: string
  duration_sec: number
  access: Access
  trial_sec: number
}

// ---------- 标签 ----------

export type TagStatus = 'active' | 'hidden'

export type Tag = {
  id: number
  name: string
  slug: string
  is_hidden: boolean
  status: TagStatus
  merged_into_id: number | null
  created_at: string
}

export type TagInput = { name: string; slug: string; is_hidden: boolean; status: TagStatus }

// ---------- 维度与取值 ----------

export type Term = {
  id: number
  taxonomy_id: number
  name: string
  slug: string
  status: ActiveStatus
  is_channel: boolean
  code: string
  sort: number
}

export type Taxonomy = {
  id: number
  name: string
  slug: string
  multi_select: boolean
  show_in_explore: boolean
  status: ActiveStatus
  sort: number
  terms: Term[]
}

export type TaxonomyInput = {
  name: string
  slug: string
  multi_select: boolean
  show_in_explore: boolean
  status: ActiveStatus
}

export type TermInput = { name: string; slug: string; status: ActiveStatus; is_channel: boolean; code: string }

// ---------- 专题 ----------

export type TopicStatus = 'draft' | 'published'
export type SourceType = 'manual' | 'auto'
export type RuleSort = 'latest' | 'hot' | 'recommend'
export type RuleWindow = 'all' | '7d' | '14d' | '30d' | '90d'

export type TopicRule = {
  term_codes: string[]
  tag_id: number | null
  sort: RuleSort
  window: RuleWindow
  limit: number
}

export type FAQ = { question: string; answer: string }

export type Topic = {
  id: number
  title: string
  slug: string
  summary: string
  status: TopicStatus
  source_type: SourceType
  rule: TopicRule
  faq: FAQ[]
  list_in_index: boolean
  created_at: string
}

export type TopicDrama = {
  id: number
  title: string
  status: DramaStatus
  display_status: DisplayStatus
  publish_at: string | null
  cover_portrait: string
}

export type TopicDetail = Topic & { dramas: TopicDrama[] }

export type TopicInput = {
  title: string
  slug: string
  summary: string
  status: TopicStatus
  source_type: SourceType
  rule: TopicRule
  faq: FAQ[]
  list_in_index: boolean
}

export type TextListParams = { q?: string; status?: string; page?: number; page_size?: number }

// ---------- 查询 ----------

export const contentKeys = {
  all: ['content'] as const,
  dramas: ['content', 'dramas'] as const,
  dramaList: (p: DramaListParams) => ['content', 'dramas', 'list', p] as const,
  drama: (id: number) => ['content', 'dramas', 'detail', id] as const,
  episodes: (dramaId: number) => ['content', 'dramas', 'episodes', dramaId] as const,
  tags: ['content', 'tags'] as const,
  tagList: (p: TextListParams & { merged?: boolean }) => ['content', 'tags', 'list', p] as const,
  taxonomies: ['content', 'taxonomies'] as const,
  topics: ['content', 'topics'] as const,
  topicList: (p: TextListParams & { source_type?: SourceType }) => ['content', 'topics', 'list', p] as const,
  topic: (id: number) => ['content', 'topics', 'detail', id] as const,
}

// 字典类数据（标签、维度）变化少（docs/06 第 0.5 节）
const DICT_STALE = 10 * 60 * 1000

export const dramaListQuery = (p: DramaListParams) =>
  queryOptions({
    queryKey: contentKeys.dramaList(p),
    queryFn: () => http.get<Page<Drama>>('/dramas', { query: p }),
    placeholderData: keepPreviousData,
  })

export const dramaQuery = (id: number) =>
  queryOptions({ queryKey: contentKeys.drama(id), queryFn: () => http.get<DramaDetail>(`/dramas/${id}`) })

export const episodesQuery = (dramaId: number) =>
  queryOptions({
    queryKey: contentKeys.episodes(dramaId),
    queryFn: () => http.get<List<Episode>>(`/dramas/${dramaId}/episodes`).then((r) => r.list),
  })

export const tagListQuery = (p: TextListParams & { merged?: boolean }) =>
  queryOptions({
    queryKey: contentKeys.tagList(p),
    queryFn: () =>
      http.get<Page<Tag>>('/tags', { query: { ...p, merged: p.merged === undefined ? undefined : String(p.merged) } }),
    placeholderData: keepPreviousData,
    staleTime: DICT_STALE,
  })

export const taxonomiesQuery = queryOptions({
  queryKey: contentKeys.taxonomies,
  queryFn: () => http.get<List<Taxonomy>>('/taxonomies').then((r) => r.list),
  staleTime: DICT_STALE,
})

export const topicListQuery = (p: TextListParams & { source_type?: SourceType }) =>
  queryOptions({
    queryKey: contentKeys.topicList(p),
    queryFn: () => http.get<Page<Topic>>('/topics', { query: p }),
    placeholderData: keepPreviousData,
  })

export const topicQuery = (id: number) =>
  queryOptions({ queryKey: contentKeys.topic(id), queryFn: () => http.get<TopicDetail>(`/topics/${id}`) })

// ---------- 写操作 ----------

export const contentApi = {
  createDrama: (body: DramaCreateInput) => http.post<Drama>('/dramas', body),
  updateDrama: (id: number, body: DramaUpdateInput) => http.patch<Drama>(`/dramas/${id}`, body),
  deleteDrama: (id: number) => http.delete<void>(`/dramas/${id}`),
  setDramaTags: (id: number, tagIds: number[]) => http.put<{ tags: Tag[] }>(`/dramas/${id}/tags`, { tag_ids: tagIds }),
  setDramaTerms: (id: number, termIds: number[]) =>
    http.put<{ terms: DramaTerm[] }>(`/dramas/${id}/terms`, { term_ids: termIds }),
  bulkDramas: (body: BulkInput) => http.post<{ affected: number }>('/dramas/bulk', body),

  createEpisode: (dramaId: number, body: EpisodeInput) => http.post<Episode>(`/dramas/${dramaId}/episodes`, body),
  updateEpisode: (id: number, body: EpisodeInput) => http.patch<Episode>(`/episodes/${id}`, body),
  deleteEpisode: (id: number) => http.delete<void>(`/episodes/${id}`),

  createTag: (body: TagInput) => http.post<Tag>('/tags', body),
  updateTag: (id: number, body: TagInput) => http.patch<Tag>(`/tags/${id}`, body),
  mergeTag: (id: number, targetId: number) =>
    http.post<{ merged_into_id: number; moved_dramas: number }>(`/tags/${id}/merge`, { target_id: targetId }),

  createTaxonomy: (body: TaxonomyInput) => http.post<Taxonomy>('/taxonomies', body),
  updateTaxonomy: (id: number, body: TaxonomyInput) => http.patch<Taxonomy>(`/taxonomies/${id}`, body),
  deleteTaxonomy: (id: number) => http.delete<void>(`/taxonomies/${id}`),
  reorderTaxonomies: (ids: number[]) => http.post<void>('/taxonomies/reorder', { ids }),
  createTerm: (taxonomyId: number, body: TermInput) => http.post<Term>(`/taxonomies/${taxonomyId}/terms`, body),
  updateTerm: (taxonomyId: number, termId: number, body: TermInput) =>
    http.patch<Term>(`/taxonomies/${taxonomyId}/terms/${termId}`, body),
  deleteTerm: (taxonomyId: number, termId: number) => http.delete<void>(`/taxonomies/${taxonomyId}/terms/${termId}`),
  reorderTerms: (taxonomyId: number, ids: number[]) => http.post<void>(`/taxonomies/${taxonomyId}/terms/reorder`, { ids }),

  createTopic: (body: TopicInput) => http.post<Topic>('/topics', body),
  updateTopic: (id: number, body: TopicInput) => http.patch<Topic>(`/topics/${id}`, body),
  deleteTopic: (id: number) => http.delete<void>(`/topics/${id}`),
  setTopicDramas: (id: number, dramaIds: number[]) =>
    http.put<{ drama_ids: number[] }>(`/topics/${id}/dramas`, { drama_ids: dramaIds }),
}

// ---------- 显示文案 ----------

export const DISPLAY_STATUS_LABELS: Record<DisplayStatus, string> = {
  published: '已上架',
  scheduled: '定时未到点',
  draft: '草稿',
  offline: '已下架',
}

export const DRAMA_STATUS_LABELS: Record<DramaStatus, string> = {
  draft: '草稿',
  published: '已上架',
  offline: '已下架',
}

export const DRAMA_SORT_LABELS: Record<DramaSort | 'default', string> = {
  default: '最新创建',
  latest: '最新上架',
  updated: '最近修改',
  hot: '最热',
  recommend: '推荐（排序值）',
}

export const ACCESS_LABELS: Record<Access, string> = { free: '免费', login: '登录可看', vip: 'VIP' }

export const MEDIA_STATUS_LABELS: Record<MediaStatus, string> = {
  pending: '没有视频',
  transcoding: '转封装中',
  ready: '可播放',
  failed: '转封装失败',
}

export const ACTIVE_LABELS: Record<ActiveStatus, string> = { active: '启用', inactive: '停用' }

export const TAG_STATUS_LABELS: Record<TagStatus, string> = { active: '正常', hidden: '隐藏' }

export const TOPIC_STATUS_LABELS: Record<TopicStatus, string> = { draft: '草稿', published: '发布' }

export const SOURCE_TYPE_LABELS: Record<SourceType, string> = { manual: '手选', auto: '规则自动' }

export const RULE_SORT_LABELS: Record<RuleSort, string> = { latest: '最新上架', hot: '最热', recommend: '推荐（排序值）' }

export const RULE_WINDOW_LABELS: Record<RuleWindow, string> = {
  all: '不限',
  '7d': '7 天内上新',
  '14d': '14 天内上新',
  '30d': '30 天内上新',
  '90d': '90 天内上新',
}
