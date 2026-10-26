import type { Drama, DramaDetail, DramaUpdateInput, Taxonomy, Term } from './api'

/** slug 规则（与后端一致）：小写字母、数字与连字符，不以连字符开头或结尾，最多 64 个字符；留空表示自动生成或不填。 */
export const SLUG_PATTERN = /^$|^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$/
export const SLUG_HINT = '小写字母、数字与连字符，不能以连字符开头或结尾'

/** 取值编号规则：字母、数字、下划线与连字符，最多 32 个字符。 */
export const CODE_PATTERN = /^[A-Za-z0-9_-]{0,32}$/

/** 把「甲, 乙，丙、丁」或多行文本拆成去重后的列表（保留首次出现的顺序）。 */
export function splitList(text: string): string[] {
  const out: string[] = []
  for (const part of text.split(/[,，、\n]/)) {
    const s = part.trim()
    if (s && !out.includes(s)) out.push(s)
  }
  return out
}

/** 列表显示为「甲、乙」，编辑时用于回填输入框。 */
export const joinList = (items: string[]) => items.join('、')

/**
 * 解析粘贴的剧编号：支持「#12, 13 14」「12，13」等写法。
 * 返回去重后的编号（保留顺序）与无法识别的片段。
 */
export function parseIdList(text: string): { ids: number[]; invalid: string[] } {
  const ids: number[] = []
  const invalid: string[] = []
  for (const token of text.split(/[\s,，、;；]+/)) {
    if (!token) continue
    const m = /^#?([1-9]\d{0,15})$/.exec(token)
    if (!m) {
      invalid.push(token)
      continue
    }
    const id = Number(m[1])
    if (!ids.includes(id)) ids.push(id)
  }
  return { ids, invalid }
}

/** 把 list[from] 移到 to 的位置；越界时原样返回。 */
export function moveItem<T>(list: readonly T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) return [...list]
  const next = [...list]
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

const pad = (n: number) => String(n).padStart(2, '0')

/** ISO 时间 → `<input type="datetime-local">` 的本地时间值（精确到分钟）。 */
export function toLocalInput(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** `datetime-local` 的本地时间值 → ISO 时间（UTC）；空值或无效值为 null。 */
export function fromLocalInput(value: string): string | null {
  if (!value) return null
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? null : d.toISOString()
}

/** 前台显示的「更新至 N 集 / 全 N 集」：只算转封装完成、可播放的分集（S1-5），所以有分集也可能是 0。 */
export function episodeSummary(d: Pick<Drama, 'latest_episode' | 'is_finished'>): string {
  if (d.latest_episode === 0) return '暂无可播分集'
  return d.is_finished ? `全 ${d.latest_episode} 集` : `更新至 ${d.latest_episode} 集`
}

/** 秒 → 「m:ss」或「h:mm:ss」；0 表示未知。 */
export function formatDuration(sec: number): string {
  if (sec <= 0) return '未知'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

/**
 * 在维度取值的选择中切换一个取值：多选维度直接增减；单选维度选中时替换该维度原来的取值，再点一次取消。
 * selected 为已选取值 ID 的列表（保持顺序）。
 */
export function toggleTerm(
  selected: readonly number[],
  taxonomy: Pick<Taxonomy, 'multi_select'> & { terms: readonly Pick<Term, 'id'>[] },
  term: Pick<Term, 'id'>
): number[] {
  if (selected.includes(term.id)) return selected.filter((id) => id !== term.id)
  if (taxonomy.multi_select) return [...selected, term.id]
  const siblings = new Set(taxonomy.terms.map((t) => t.id))
  return [...selected.filter((id) => !siblings.has(id)), term.id]
}

/** 剧资料表单的值（字符串形式，便于输入框编辑）。 */
export type DramaForm = {
  title: string
  aliases: string
  slug: string
  summary: string
  cover_portrait: string
  cover_landscape: string
  badge: string
  score: string
  year: string
  status: DramaUpdateInput['status']
  publish_at: string
  is_finished: boolean
  sort_weight: string
  view_base: string
  seo_title: string
  seo_desc: string
}

/** 剧资料 → 表单值。 */
export function dramaToForm(d: DramaDetail | Drama): DramaForm {
  return {
    title: d.title,
    aliases: joinList(d.aliases),
    slug: d.slug,
    summary: d.summary,
    cover_portrait: d.cover_portrait,
    cover_landscape: d.cover_landscape,
    badge: d.badge,
    score: d.score === null ? '' : String(d.score),
    year: d.year === null ? '' : String(d.year),
    status: d.status,
    publish_at: toLocalInput(d.publish_at),
    is_finished: d.is_finished,
    sort_weight: String(d.sort_weight),
    view_base: String(d.view_base),
    seo_title: d.seo_title,
    seo_desc: d.seo_desc,
  }
}

/** 数字输入框的值：留空为 null。 */
const num = (s: string) => (s.trim() === '' ? null : Number(s))

/** 表单值 → 整体保存的请求体（已通过校验）。数字留空时：评分、年份为 null，排序值与初始播放量为 0。 */
export function formToDramaInput(f: DramaForm): DramaUpdateInput {
  return {
    title: f.title.trim(),
    aliases: splitList(f.aliases),
    slug: f.slug.trim().toLowerCase(),
    summary: f.summary.trim(),
    cover_portrait: f.cover_portrait.trim(),
    cover_landscape: f.cover_landscape.trim(),
    badge: f.badge.trim(),
    score: num(f.score),
    year: num(f.year),
    status: f.status,
    publish_at: fromLocalInput(f.publish_at),
    is_finished: f.is_finished,
    sort_weight: num(f.sort_weight) ?? 0,
    view_base: num(f.view_base) ?? 0,
    seo_title: f.seo_title.trim(),
    seo_desc: f.seo_desc.trim(),
  }
}

/** 两个 ID 列表是否相同（顺序也相同）。 */
export const sameOrder = (a: readonly number[], b: readonly number[]) =>
  a.length === b.length && a.every((id, i) => id === b[i])

/** 两个 ID 列表的元素是否相同（不计顺序）。 */
export const sameMembers = (a: readonly number[], b: readonly number[]) => {
  if (a.length !== b.length) return false
  const set = new Set(a)
  return b.every((id) => set.has(id))
}

/** 关联选择会调用其他页面的读接口，需要该页面的「查看」权限（docs/03 第 2.5 节）。 */
const LOOKUP_PAGE_NAMES = { dramas: '剧集', tags: '标签', taxonomies: '筛选维度', topics: '专题' } as const
export type LookupPage = keyof typeof LOOKUP_PAGE_NAMES

/**
 * 没有 page 的查看权限时，返回禁用关联选择的原因；有权限返回 null。
 * 用于不发出注定 403 的请求（任何读请求 403 都会跳到无权限页）。
 */
export function lookupReason(can: (code: string) => boolean, page: LookupPage): string | null {
  return can(`${page}.view`) ? null : `需要「${LOOKUP_PAGE_NAMES[page]}」的查看权限`
}
