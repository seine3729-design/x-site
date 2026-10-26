import { http } from '@/lib/http'
import { DISPLAY_STATUS_LABELS, type Drama, type Page } from './api'
import type { PickerOption } from './components/search-picker'

/** 按剧名、别名或 #编号搜索剧（不含回收站中的剧），供专题手选、批量加入等选择剧的地方使用。 */
export async function searchDramas(q: string): Promise<PickerOption[]> {
  const page = await http.get<Page<Drama>>('/dramas', { query: { q: q || undefined, page_size: 20 } })
  return page.list.map((d) => ({
    value: d.id,
    label: d.title,
    hint: `#${d.id} · ${DISPLAY_STATUS_LABELS[d.display_status]}`,
  }))
}
