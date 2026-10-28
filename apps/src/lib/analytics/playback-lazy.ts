/**
 * 播放埋点的懒加载外壳（W4-1）：播放器内核同步调用这里返回的函数，口径换算（playback.ts）连同上报队列
 * 经 runtime.ts 动态导入，不进播放页与刷剧页的首屏包（docs/08：首屏 JS ≤ 90 KB br）。
 * 导入完成之前的调用连同发生时刻、页面是否可见一起记下，导入后按原时刻重放，首帧耗时与观看时长不受影响。
 */
import type { PlaybackCall, PlaybackMethod, PlaybackStats, PlaybackStatsOptions, PlaySource } from './playback.ts';

/** 播放器内核调用埋点的入口：方法名 + 参数，与 PlaybackStats 的同名方法一一对应 */
export type PlaybackHook = <M extends PlaybackMethod>(method: M, ...args: Parameters<PlaybackStats[M]>) => void;

export function lazyPlaybackStats(src: PlaySource, options: Pick<PlaybackStatsOptions, 'emit'> = {}): PlaybackHook {
	// 播放器在服务端渲染时也会被构造：那时什么都不做
	if (typeof document === 'undefined') {
		return () => {};
	}
	const visibleNow = () => document.visibilityState !== 'hidden';
	let real: ((method: PlaybackMethod, args: unknown[]) => void) | null = null;
	let calls: PlaybackCall[] | null = [];
	const record = (method: PlaybackMethod, args: unknown[]) => {
		calls?.push([method, args, performance.now(), visibleNow()]);
	};
	// 导入完成之前的可见性变化也要记下（之后由 PlaybackStats 自己监听）
	const onVisibility = () => record('visibility', [visibleNow()]);
	document.addEventListener('visibilitychange', onVisibility);

	import('./runtime.ts')
		.then((runtime) => {
			real = runtime.replayPlayback(src, calls ?? [], options);
		})
		.catch(() => {
			// 埋点模块加载失败（如断网）：不影响播放，这一路埋点放弃
		})
		.finally(() => {
			document.removeEventListener('visibilitychange', onVisibility);
			calls = null;
		});

	return (method, ...args) => {
		if (real) {
			real(method, args);
		} else {
			record(method, args);
		}
	};
}
