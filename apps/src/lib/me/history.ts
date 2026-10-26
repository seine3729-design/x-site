/**
 * 观看历史：登录用户以账号为准（GET /api/v1/me/history），游客只有本机记录（IndexedDB）。
 * 两者统一成同一个结构给页面用。
 */
import type { HistoryItem } from '#lib/api/types.ts';
import { localHistory } from '#lib/local/store.ts';

export type HistoryRow = HistoryItem & { updatedMs: number };

export async function loadHistory(loggedIn: boolean): Promise<HistoryRow[]> {
	if (loggedIn) {
		const { api } = await import('#lib/api/client.ts');
		const data = await api.get<{ items: HistoryItem[] }>('/api/v1/me/history');
		return data.items.map((item) => ({ ...item, updatedMs: Date.parse(item.updated_at) || 0 }));
	}
	const rows = await localHistory.list();
	return rows.map((row) => ({
		drama_id: row.drama_id,
		title: row.title,
		cover: row.cover,
		episode_id: row.episode_id,
		episode_number: row.episode_number,
		position_sec: row.position_sec,
		duration_sec: row.duration_sec,
		latest_episode: row.latest_episode,
		updated_at: new Date(row.updated_at).toISOString(),
		updatedMs: row.updated_at
	}));
}

/** 删除几部剧的历史（ids 为空表示清空）；本机记录同步删除 */
export async function deleteHistory(loggedIn: boolean, ids: number[]): Promise<void> {
	if (loggedIn) {
		const { api } = await import('#lib/api/client.ts');
		await api.del(ids.length === 0 ? '/api/v1/me/history?all=1' : `/api/v1/me/history?drama_ids=${ids.join(',')}`);
	}
	if (ids.length === 0) {
		await localHistory.clear();
	} else {
		await Promise.all(ids.map((id) => localHistory.remove(id)));
	}
}

/** 进度 0–1；时长未知时按 0 */
export function progressOf(row: Pick<HistoryItem, 'position_sec' | 'duration_sec'>): number {
	return row.duration_sec > 0 ? Math.min(1, row.position_sec / row.duration_sec) : 0;
}
