import type { BrandingOption } from './branding-api'

/** 模板里的 {变量}。 */
const TEMPLATE_VAR = /\{([^{}]*)\}/g

/**
 * 找出模板里未登记的变量名。
 * 变量名写错（如 {dramaTitle}）后端会拒绝保存，前端提前标出来，省一次失败往返；
 * 更重要的是：真漏到线上，前台会把 "{dramaTitle}" 原样显示在标题里给用户看。
 */
export function unknownVariables(template: string, variables: readonly BrandingOption[]): string[] {
  const known = new Set(variables.map((v) => v.value))
  const out: string[] = []
  for (const m of template.matchAll(TEMPLATE_VAR)) {
    const name = m[1].trim()
    if (!known.has(name) && !out.includes(m[1])) out.push(m[1])
  }
  return out
}

/** 在光标处插入变量；没有选区时追加到末尾。返回新文本与插入后的光标位置。 */
export function insertVariable(text: string, variable: string, start?: number, end?: number): { value: string; cursor: number } {
  const token = `{${variable}}`
  const from = start ?? text.length
  const to = end ?? from
  const value = text.slice(0, from) + token + text.slice(to)
  return { value, cursor: from + token.length }
}

/** 模板预览：把变量替换成示例值，让运营看出来标题大概长什么样。 */
const SAMPLES: Record<string, string> = {
  site_name: '短剧站',
  slogan: '每天追好剧',
  drama_title: '夏夜未眠',
  summary: '一个关于重逢的故事',
  total_episodes: '24',
  episode_no: '3',
  channel_name: '都市',
  topic_name: '年度热播',
  tag_name: '甜宠',
  count: '128',
  keyword: '甜宠',
  board_name: '周榜',
}

export function previewTemplate(template: string): string {
  return template.replace(TEMPLATE_VAR, (whole, name: string) => SAMPLES[name.trim()] ?? whole)
}

/** 站长验证的 name：字母、数字、点、下划线与连字符。 */
export function isVerifyName(value: string): boolean {
  const s = value.trim()
  return s !== '' && s.length <= 60 && /^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(s)
}

/** 表单里的模板是「页面 → 文本」，提交前去掉空值（与后端保存结果一致：留空表示用前台默认）。 */
export function cleanTemplates(templates: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [page, text] of Object.entries(templates)) {
    const v = text.trim()
    if (v !== '') out[page] = v
  }
  return out
}

/** 提交前丢掉整行留空的站长验证（前端的空白行）。 */
export function cleanVerifications<T extends { name: string; content: string }>(list: T[]): T[] {
  return list.map((v) => ({ ...v, name: v.name.trim(), content: v.content.trim() })).filter((v) => v.name !== '' || v.content !== '')
}
