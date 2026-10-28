/**
 * 注册墙漏斗（W4-1，docs/04 C.6）：曝光 → 点注册 → 成功。
 * 曝光与点击由播放器上报（#lib/analytics/playback.ts）；点击时在本标签页记下是从哪一集的墙过来的，
 * 注册成功时据此把 register 事件归到那一集（from: wall），否则算直接注册（from: direct）。
 * 只存剧 / 集编号与时间，不含身份信息；sessionStorage 关掉标签页即清空。
 */
import { flushEvents, track, type TrackFields } from './track.ts';

const KEY = 'xs_rw';
/** 点完注册墙后多久之内注册成功才算墙带来的 */
const WINDOW_MS = 30 * 60_000;

type WallClick = { d: number; e: number; t: number };

export function rememberWallClick(dramaId: number, episodeId: number) {
	try {
		sessionStorage.setItem(KEY, JSON.stringify({ d: dramaId, e: episodeId, t: Date.now() } satisfies WallClick));
	} catch {
		// 写不进去（隐私模式）：注册仍会上报，只是算成直接注册
	}
}

/** 由本标签页记下的点击（原始字符串）算出 register 事件的字段：30 分钟内点过注册墙算墙带来的 */
export function registerFields(raw: string | null, now: number): TrackFields {
	try {
		const value = raw ? (JSON.parse(raw) as Partial<WallClick>) : null;
		if (value && typeof value.t === 'number' && now - value.t >= 0 && now - value.t <= WINDOW_MS) {
			return { drama_id: Number(value.d) || undefined, episode_id: Number(value.e) || undefined, props: { from: 'wall' } };
		}
	} catch {
		// 内容损坏：按直接注册处理
	}
	return { props: { from: 'direct' } };
}

/** 注册成功后调用（注册页表单动作返回跳转时） */
export function trackRegistered() {
	let raw: string | null = null;
	try {
		raw = sessionStorage.getItem(KEY);
		sessionStorage.removeItem(KEY);
	} catch {
		// 读不到：按直接注册处理
	}
	track('register', registerFields(raw, Date.now()));
	// 紧接着会跳转页面，先发出去
	flushEvents();
}
