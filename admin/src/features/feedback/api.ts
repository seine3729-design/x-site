import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/**
 * 反馈社区的后台接口（feedback/docs/07，契约 service/api/openapi.yaml 0.17.0 的 Fb* schema）。
 * 反馈社区与短剧业务共用一个后端，表前缀 fb_（feedback/docs/adr/0008）。
 * 类型与契约逐字段对应；X0-5 接入生成的类型后替换。
 */

// ---------- 通用 ----------

export type Page<T> = { list: T[]; total: number; page: number; page_size: number }

export const DEFAULT_PAGE_SIZE = 20

// ---------- 枚举与中文文案 ----------

export type FbStatus =
  | 'submitted'
  | 'triaged'
  | 'planned'
  | 'in_progress'
  | 'verifying'
  | 'shipped'
  | 'closed'
  | 'duplicate'
  | 'declined'

/** 状态文案与给运营看的语义说明（feedback/docs/adr/0004）。 */
export const STATUS_LABELS: Record<FbStatus, string> = {
  submitted: '已收到',
  triaged: '已确认',
  planned: '已进入计划',
  in_progress: '正在处理',
  verifying: '等待验证',
  shipped: '已上线',
  closed: '已关闭',
  duplicate: '重复反馈',
  declined: '暂不采纳',
}

/** 合法状态流转，与后端 domain/feedback 的状态机一致；用于只显示可选的下一步。 */
export const STATUS_TRANSITIONS: Record<FbStatus, FbStatus[]> = {
  submitted: ['triaged', 'duplicate', 'declined', 'closed'],
  triaged: ['planned', 'in_progress', 'duplicate', 'declined', 'closed'],
  planned: ['in_progress', 'triaged', 'declined', 'closed'],
  in_progress: ['verifying', 'shipped', 'planned', 'closed'],
  verifying: ['shipped', 'in_progress', 'closed'],
  shipped: ['closed', 'triaged'],
  closed: ['triaged'],
  duplicate: ['triaged'],
  declined: ['triaged'],
}

export type FbKind = 'bug' | 'experience' | 'feature' | 'content' | 'access' | 'competitor' | 'other'

export const KIND_LABELS: Record<FbKind, string> = {
  bug: 'Bug',
  experience: '体验问题',
  feature: '功能建议',
  content: '内容需求',
  access: '无法访问',
  competitor: '竞品发现',
  other: '其他',
}

/** 反馈来源：社区用户自己提交，或外部网站经 API Key 送入（feedback/docs/adr/0009）。 */
export type FbSource = 'community' | 'open_api'

export const SOURCE_LABELS: Record<FbSource, string> = {
  community: '社区提交',
  open_api: '外部网站',
}

export type FbPriority = 'critical' | 'high' | 'medium' | 'low' | 'unset'

export const PRIORITY_LABELS: Record<FbPriority, string> = {
  critical: '最高',
  high: '高',
  medium: '中',
  low: '低',
  unset: '未定',
}

export type FbVisibility = 'public' | 'members' | 'private'

export const VISIBILITY_LABELS: Record<FbVisibility, string> = {
  public: '公开',
  members: '仅成员',
  private: '私密',
}

export type FbProjectStatus = 'draft' | 'recruiting' | 'active' | 'paused' | 'archived'

export const PROJECT_STATUS_LABELS: Record<FbProjectStatus, string> = {
  draft: '草稿',
  recruiting: '招募中',
  active: '进行中',
  paused: '已暂停',
  archived: '已归档',
}

export type FbJoinMode = 'open' | 'application' | 'invite_only' | 'closed'

export const JOIN_MODE_LABELS: Record<FbJoinMode, string> = {
  open: '可自助加入',
  application: '需审核',
  invite_only: '仅邀请码',
  closed: '不接受新成员',
}

export type FbMemberRole = 'member' | 'operator' | 'maintainer' | 'owner'

export const MEMBER_ROLE_LABELS: Record<FbMemberRole, string> = {
  member: '测试成员',
  operator: '项目运营',
  maintainer: '维护者',
  owner: '所有者',
}

export type FbUserStatus = 'active' | 'restricted' | 'suspended'

export const USER_STATUS_LABELS: Record<FbUserStatus, string> = {
  active: '正常',
  restricted: '只读',
  suspended: '已封禁',
}

export type FbPostKind = 'announcement' | 'test_task' | 'changelog'

export const POST_KIND_LABELS: Record<FbPostKind, string> = {
  announcement: '公告',
  test_task: '测试任务',
  changelog: '上线小结',
}

export type FbPostStatus = 'draft' | 'published' | 'hidden'

export const POST_STATUS_LABELS: Record<FbPostStatus, string> = {
  draft: '草稿',
  published: '已发布',
  hidden: '已隐藏',
}

/** 议题状态：open 就是前台显示的「征集中」。 */
export type FbTopicStatus = 'draft' | 'open' | 'closed' | 'archived'

export const TOPIC_STATUS_LABELS: Record<FbTopicStatus, string> = {
  draft: '草稿',
  open: '征集中',
  closed: '已结束',
  archived: '已归档',
}

export type FbAnswerStatus = 'visible' | 'hidden'

export const ANSWER_STATUS_LABELS: Record<FbAnswerStatus, string> = {
  visible: '显示中',
  hidden: '已隐藏',
}

export type FbApiKeyStatus = 'active' | 'inactive'

export const API_KEY_STATUS_LABELS: Record<FbApiKeyStatus, string> = {
  active: '启用',
  inactive: '停用',
}

// ---------- 实体 ----------

export type FbProject = {
  id: number
  slug: string
  key_prefix: string
  name: string
  summary: string
  description: string
  cover: string
  try_url: string
  status: FbProjectStatus
  visibility: 'public' | 'members'
  join_mode: FbJoinMode
  sort: number
  created_at: string
  updated_at: string
}

export type FbProjectInput = {
  slug: string
  key_prefix: string
  name: string
  summary: string
  description: string
  cover: string
  try_url: string
  status: FbProjectStatus
  visibility: 'public' | 'members'
  join_mode: FbJoinMode
  sort: number
}

export type FbMember = {
  project_id: number
  user_id: number
  handle: string
  nickname: string
  user_status: FbUserStatus
  role: FbMemberRole
  status: 'pending' | 'active' | 'removed'
  joined_at: string
}

export type FbInvite = {
  id: number
  code_hint: string
  project_id?: number
  project_name?: string
  role: FbMemberRole
  max_uses: number
  used_count: number
  note: string
  expires_at: string | null
  revoked_at: string | null
  created_at: string
}

export type FbInviteInput = {
  project_id?: number
  role: FbMemberRole
  max_uses: number
  note: string
  expires_at: string | null
}

export type FbPost = {
  id: number
  project_id: number
  project_name?: string
  project_slug?: string
  kind: FbPostKind
  title: string
  body: string
  status: FbPostStatus
  visibility: 'public' | 'members'
  pinned: boolean
  published_at: string | null
  created_at: string
  updated_at: string
}

export type FbPostInput = {
  project_id: number
  kind: FbPostKind
  title: string
  body: string
  status: FbPostStatus
  visibility: 'public' | 'members'
  pinned: boolean
}

export type FbFeedback = {
  id: number
  project_id: number
  project_slug?: string
  project_name?: string
  number: number
  key: string
  author_id?: number
  author_handle?: string
  author_nickname?: string
  kind: FbKind
  title: string
  body: string
  env: string
  visibility: FbVisibility
  status: FbStatus
  priority: FbPriority
  duplicate_of?: number
  vote_count: number
  voted: boolean
  source: FbSource
  /** source=open_api 时的来源密钥。 */
  api_key_id?: number
  page_url?: string
  /** 提交者自愿留的联系方式，只在后台响应里出现。 */
  contact?: string
  version: number
  last_activity_at: string
  created_at: string
}

export type FbEvent = {
  id: number
  from_status: string
  to_status: FbStatus
  note: string
  internal_note?: string
  actor_name?: string
  created_at: string
}

export type FbFeedbackDetail = { feedback: FbFeedback; events: FbEvent[] }

export type FbUser = {
  id: number
  handle: string
  nickname: string
  bio: string
  show_level: boolean
  status: FbUserStatus
  points: number
  created_at: string
}

export type FbTopic = {
  id: number
  project_id: number
  project_name?: string
  project_slug?: string
  project_cover?: string
  title: string
  body: string
  answer_hint: string
  status: FbTopicStatus
  visibility: 'public' | 'members'
  pinned: boolean
  answer_count: number
  ends_at: string | null
  published_at: string | null
  created_at: string
  updated_at: string
}

export type FbTopicInput = {
  project_id: number
  title: string
  body: string
  answer_hint: string
  status: FbTopicStatus
  visibility: 'public' | 'members'
  pinned: boolean
  ends_at: string | null
}

/** 一条议题回答。署名用内测编号（tester_no），不暴露社区用户 ID。 */
export type FbAnswer = {
  id: number
  topic_id: number
  parent_id?: number
  tester_no?: number
  handle?: string
  nickname?: string
  body: string
  featured: boolean
  helpful_count: number
  helpful_by_me: boolean
  status?: FbAnswerStatus
  created_at: string
}

/** 开放提交接口的密钥。明文只在创建响应里出现一次。 */
export type FbApiKey = {
  id: number
  project_id: number
  project_name?: string
  project_slug?: string
  name: string
  key_hint: string
  default_visibility: FbVisibility
  allowed_origins: string[]
  daily_limit: number
  status: FbApiKeyStatus
  used_count: number
  last_used_at: string | null
  revoked_at: string | null
  created_at: string
}

export type FbApiKeyInput = {
  project_id: number
  name: string
  default_visibility: FbVisibility
  allowed_origins: string[]
  daily_limit: number
  status: FbApiKeyStatus
}

export type FbTriageInput = {
  status?: FbStatus
  priority?: FbPriority
  visibility?: FbVisibility
  duplicate_of?: number
  note?: string
  internal_note?: string
  version: number
}

export type StatusCount = { status: FbStatus; total: number }

// ---------- 列表参数 ----------

export type ProjectListParams = { q?: string; status?: FbProjectStatus; page: number; page_size: number }
export type IssueListParams = {
  project_id?: number
  status?: FbStatus
  kind?: FbKind
  priority?: FbPriority
  visibility?: FbVisibility
  source?: FbSource
  q?: string
  sort?: 'latest' | 'newest' | 'votes' | 'oldest'
  page: number
  page_size: number
}
export type PostListParams = { project_id?: number; status?: FbPostStatus; page: number; page_size: number }
export type InviteListParams = { project_id?: number; page: number; page_size: number }
export type UserListParams = { q?: string; status?: FbUserStatus; page: number; page_size: number }
export type TopicListParams = {
  project_id?: number
  status?: FbTopicStatus
  q?: string
  page: number
  page_size: number
}
export type ApiKeyListParams = { project_id?: number; page: number; page_size: number }

// ---------- query key ----------

export const feedbackKeys = {
  all: ['feedback'] as const,
  projects: ['feedback', 'projects'] as const,
  members: (projectID: number) => ['feedback', 'projects', projectID, 'members'] as const,
  issues: ['feedback', 'issues'] as const,
  issue: (id: number) => ['feedback', 'issues', id] as const,
  overview: ['feedback', 'issues', 'overview'] as const,
  posts: ['feedback', 'posts'] as const,
  invites: ['feedback', 'invites'] as const,
  users: ['feedback', 'users'] as const,
  topics: ['feedback', 'topics'] as const,
  answers: (topicID: number) => ['feedback', 'topics', topicID, 'answers'] as const,
  apiKeys: ['feedback', 'api-keys'] as const,
}

// ---------- 接口 ----------

export const feedbackApi = {
  listProjects: (p: ProjectListParams) => http.get<Page<FbProject>>('/feedback/projects', { query: { ...p } }),
  createProject: (body: FbProjectInput) => http.post<FbProject>('/feedback/projects', body),
  updateProject: (id: number, body: FbProjectInput) => http.patch<FbProject>(`/feedback/projects/${id}`, body),
  deleteProject: (id: number) => http.delete<null>(`/feedback/projects/${id}`),

  listMembers: (projectID: number, page: number, pageSize: number) =>
    http.get<Page<FbMember>>(`/feedback/projects/${projectID}/members`, { query: { page, page_size: pageSize } }),
  setMemberRole: (projectID: number, userID: number, body: { role: FbMemberRole; remove?: boolean }) =>
    http.patch<FbMember>(`/feedback/projects/${projectID}/members/${userID}`, body),

  listIssues: (p: IssueListParams) => http.get<Page<FbFeedback>>('/feedback/issues', { query: { ...p } }),
  getIssue: (id: number) => http.get<FbFeedbackDetail>(`/feedback/issues/${id}`),
  triage: (id: number, body: FbTriageInput) => http.post<FbFeedback>(`/feedback/issues/${id}/triage`, body),
  overview: (projectID?: number) =>
    http.get<{ list: StatusCount[] }>('/feedback/issues/overview', { query: { project_id: projectID } }),

  listPosts: (p: PostListParams) => http.get<Page<FbPost>>('/feedback/posts', { query: { ...p } }),
  createPost: (body: FbPostInput) => http.post<FbPost>('/feedback/posts', body),
  updatePost: (id: number, body: FbPostInput) => http.patch<FbPost>(`/feedback/posts/${id}`, body),
  deletePost: (id: number) => http.delete<null>(`/feedback/posts/${id}`),

  listInvites: (p: InviteListParams) => http.get<Page<FbInvite>>('/feedback/invites', { query: { ...p } }),
  createInvite: (body: FbInviteInput) => http.post<{ invite: FbInvite; code: string }>('/feedback/invites', body),
  revokeInvite: (id: number) => http.post<FbInvite>(`/feedback/invites/${id}/revoke`),

  listUsers: (p: UserListParams) => http.get<Page<FbUser>>('/feedback/users', { query: { ...p } }),
  setUserStatus: (id: number, status: FbUserStatus) => http.post<FbUser>(`/feedback/users/${id}/status`, { status }),

  listTopics: (p: TopicListParams) => http.get<Page<FbTopic>>('/feedback/topics', { query: { ...p } }),
  createTopic: (body: FbTopicInput) => http.post<FbTopic>('/feedback/topics', body),
  updateTopic: (id: number, body: FbTopicInput) => http.patch<FbTopic>(`/feedback/topics/${id}`, body),
  deleteTopic: (id: number) => http.delete<null>(`/feedback/topics/${id}`),
  listAnswers: (topicID: number, page: number, pageSize: number) =>
    http.get<Page<FbAnswer>>(`/feedback/topics/${topicID}/answers`, { query: { page, page_size: pageSize } }),
  setAnswerFeatured: (id: number, featured: boolean) =>
    http.post<FbAnswer>(`/feedback/answers/${id}/featured`, { featured }),
  setAnswerStatus: (id: number, status: FbAnswerStatus) =>
    http.post<FbAnswer>(`/feedback/answers/${id}/status`, { status }),

  listApiKeys: (p: ApiKeyListParams) => http.get<Page<FbApiKey>>('/feedback/api-keys', { query: { ...p } }),
  createApiKey: (body: FbApiKeyInput) =>
    http.post<{ key: FbApiKey; raw_key: string }>('/feedback/api-keys', body),
  updateApiKey: (id: number, body: FbApiKeyInput) => http.patch<FbApiKey>(`/feedback/api-keys/${id}`, body),
  revokeApiKey: (id: number) => http.post<FbApiKey>(`/feedback/api-keys/${id}/revoke`),
}

// ---------- query options ----------

export const projectListQuery = (p: ProjectListParams) =>
  queryOptions({
    queryKey: [...feedbackKeys.projects, p],
    queryFn: () => feedbackApi.listProjects(p),
    placeholderData: keepPreviousData,
  })

/** 项目下拉（筛选与表单共用），一次取够当前规模的项目。 */
export const allProjectsQuery = queryOptions({
  queryKey: [...feedbackKeys.projects, 'all'],
  queryFn: () => feedbackApi.listProjects({ page: 1, page_size: 100 }),
  staleTime: 10 * 60 * 1000,
})

export const memberListQuery = (projectID: number, page: number, pageSize: number) =>
  queryOptions({
    queryKey: [...feedbackKeys.members(projectID), page, pageSize],
    queryFn: () => feedbackApi.listMembers(projectID, page, pageSize),
    placeholderData: keepPreviousData,
  })

export const issueListQuery = (p: IssueListParams) =>
  queryOptions({
    queryKey: [...feedbackKeys.issues, p],
    queryFn: () => feedbackApi.listIssues(p),
    placeholderData: keepPreviousData,
  })

export const issueDetailQuery = (id: number) =>
  queryOptions({ queryKey: feedbackKeys.issue(id), queryFn: () => feedbackApi.getIssue(id) })

export const overviewQuery = (projectID?: number) =>
  queryOptions({
    queryKey: [...feedbackKeys.overview, projectID ?? 0],
    queryFn: () => feedbackApi.overview(projectID),
    staleTime: 60 * 1000,
  })

export const postListQuery = (p: PostListParams) =>
  queryOptions({
    queryKey: [...feedbackKeys.posts, p],
    queryFn: () => feedbackApi.listPosts(p),
    placeholderData: keepPreviousData,
  })

export const inviteListQuery = (p: InviteListParams) =>
  queryOptions({
    queryKey: [...feedbackKeys.invites, p],
    queryFn: () => feedbackApi.listInvites(p),
    placeholderData: keepPreviousData,
  })

export const userListQuery = (p: UserListParams) =>
  queryOptions({
    queryKey: [...feedbackKeys.users, p],
    queryFn: () => feedbackApi.listUsers(p),
    placeholderData: keepPreviousData,
  })

export const topicListQuery = (p: TopicListParams) =>
  queryOptions({
    queryKey: [...feedbackKeys.topics, p],
    queryFn: () => feedbackApi.listTopics(p),
    placeholderData: keepPreviousData,
  })

export const answerListQuery = (topicID: number, page: number, pageSize: number) =>
  queryOptions({
    queryKey: [...feedbackKeys.answers(topicID), page, pageSize],
    queryFn: () => feedbackApi.listAnswers(topicID, page, pageSize),
    placeholderData: keepPreviousData,
  })

export const apiKeyListQuery = (p: ApiKeyListParams) =>
  queryOptions({
    queryKey: [...feedbackKeys.apiKeys, p],
    queryFn: () => feedbackApi.listApiKeys(p),
    placeholderData: keepPreviousData,
  })

/** 把枚举文案表转成下拉选项。 */
export function optionsOf<T extends string>(labels: Record<T, string>): { label: string; value: T }[] {
  return (Object.entries(labels) as [T, string][]).map(([value, label]) => ({ label, value }))
}
