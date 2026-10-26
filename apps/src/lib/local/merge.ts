/**
 * 登录或注册成功后把游客本机数据并入账号（docs/04 B.11、docs/07 POST /api/v1/me/local-merge）。
 *
 * 预留：后端接口随 S2-10 实现，当前返回 404，按「还不能合并」处理、本机数据原样保留，下次登录再试。
 * 请求体按 docs/13 第 2 节的约定（每类 ≤ 300 条，幂等）：
 *   { history: HistoryEntry[], follows: number[], likes: { type: 'drama' | 'episode', id: number }[] }
 * 合并成功后本机数据继续作为账号缓存使用（详情页的追剧、点赞状态读这里）。
 */
import { LOCAL_LIMIT, localFollows, localHistory } from './store.ts';

const DONE_KEY = 'xs_local_merged';

export async function mergeLocalData(): Promise<void> {
	try {
		const [history, follows] = await Promise.all([localHistory.list(), localFollows.list()]);
		if (history.length === 0 && follows.length === 0) {
			return;
		}
		const signature = `${history.length}:${follows.length}:${history[0]?.updated_at ?? 0}`;
		if (localStorage.getItem(DONE_KEY) === signature) {
			return;
		}
		const { api, reserved } = await import('#lib/api/client.ts');
		const merged = await reserved(
			api.post('/api/v1/me/local-merge', {
				history: history.slice(0, LOCAL_LIMIT).map((h) => ({
					drama_id: h.drama_id,
					episode_id: h.episode_id,
					episode_number: h.episode_number,
					position_sec: h.position_sec,
					duration_sec: h.duration_sec
				})),
				follows: follows.slice(0, LOCAL_LIMIT).map((f) => f.id),
				likes: []
			}).then(() => true),
			false
		);
		if (merged) {
			localStorage.setItem(DONE_KEY, signature);
		}
	} catch {
		// 合并失败不影响登录，本机数据还在，下次再试
	}
}
