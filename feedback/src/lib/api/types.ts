import type { components } from './schema'

/**
 * 契约类型的使用入口。
 *
 * 所有接口类型都来自 service/api/openapi.yaml 生成的 schema.ts（`bun run gen:api`），
 * 这里只做改名：契约里改了字段，这些别名会直接在 typecheck 阶段报错，
 * 不会出现「前端按旧字段渲染、页面悄悄空白」的情况。
 */
type S = components['schemas']

// ---------- 枚举 ----------
export type FbStatus = S['FbStatus']
export type FbKind = S['FbKind']
export type FbPriority = S['FbPriority']
export type FbVisibility = S['FbVisibility']
export type FbProjectStatus = S['FbProjectStatus']
export type FbJoinMode = S['FbJoinMode']
export type FbMemberRole = S['FbMemberRole']
export type FbUserStatus = S['FbUserStatus']
export type FbTopicStatus = S['FbTopicStatus']
export type FbSource = S['FbSource']
/** 动态类型在契约里是 FbPost 的内联枚举，没有独立 schema，所以从实体上取。 */
export type FbPostKind = NonNullable<S['FbPost']['kind']>

// ---------- 实体 ----------
export type FbUser = S['FbUser']
export type FbProject = S['FbProject']
export type FbProjectView = S['FbProjectView']
export type FbPost = S['FbPost']
export type FbFeedback = S['FbFeedback']
export type FbFeedbackEvent = S['FbFeedbackEvent']
export type FbFeedbackDetail = S['FbFeedbackDetail']
export type FbTopic = S['FbTopic']
export type FbAnswer = S['FbAnswer']
export type FbLeaderboardEntry = S['FbLeaderboardEntry']
export type FbAuthResult = S['FbAuthResult']

// ---------- 请求体 ----------
export type FbRegisterRequest = S['FbRegisterRequest']
export type FbLoginRequest = S['FbLoginRequest']
// 没有 FbRecoverRequest：恢复码与「忘记密码」已删除（adr/0011）
export type FbSubmitRequest = S['FbSubmitRequest']
export type FbAnswerRequest = S['FbAnswerRequest']

/** 后端页码分页的统一形状（list / total / page / page_size）。 */
export type Page<T> = {
  list: T[]
  total: number
  page: number
  page_size: number
}
