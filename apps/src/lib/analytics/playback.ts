/**
 * 播放埋点（W4-1，docs/04 C.10、docs/03 第 11 节「核心观看」「播放体验 QoS」）。
 * 由播放器内核（#lib/player/controller.svelte.ts）在各个时刻调用，这里只做口径换算与上报：
 *
 * - 播放尝试 play_attempt：有播放意图（自动起播、刷剧滑到这一条）且选源成功时计一次；
 *   选源因技术原因失败也计一次（随后是 play_error）。登录 / 会员墙、分集不可播放（404）不算尝试——
 *   那是权限与内容状态，不是播放质量。
 *   尝试结果三分类由汇总推导：成功（play_start）、失败（stage=pre 的 play_error）、其余为起播前流失。
 * - 首帧 play_start.ms 从播放意图起算（含选源、清单、首个切片），刷剧预加载命中时自然很小。
 * - 失败原因三分类：source（选源与播放令牌）、network（清单 / 切片 / 密钥加载）、decode（解码与浏览器不支持）；
 *   stage 区分起播前（pre）与起播后中断（post）。
 * - 卡顿 play_stall：起播后、不是拖进度引起的缓冲等待，恢复播放时报时长。
 * - 观看 watch：只累计「真在播且页面可见」的墙钟时间（倍速不放大、后台播放不算）；
 *   每集累计满 10 秒才算有效观看，之后连同前面的秒数一起上报，此后在暂停、播完、换集、滑走、页面隐藏时结算，
 *   连续播放每满 30 秒先报一段。
 * - 刷剧流会预加载上下两条：没有播放意图之前不发任何带剧集编号的事件，否则剧集「观看人数」会被预加载虚增；
 *   预加载期间遇到的失败与登录墙先记着，等用户真的滑到这一条再报。
 *
 * 本模块经 runtime.ts 动态导入（见 playback-lazy.ts），不进首屏包。字段用 TS 的 private 而不是 #：
 * 构建目标会把 # 私有字段降级成一组辅助函数，懒加载的代码一用到，打包器就把它们从主包里拆成单独的文件，
 * 首屏反而多一个请求、多几百字节。
 */
import { rememberWallClick } from './funnel.ts';
import { track, type EventProps, type EventType, type TrackFields } from './track.ts';

export type PlaySource = 'play' | 'feed';
export type PlayFailure = {
	reason: 'source' | 'network' | 'decode';
	/** HTTP 状态码（有的话） */
	code?: number;
	/** 更细的原因（hls.js 的错误详情、unsupported 等），便于排查 */
	detail?: string;
	lineId?: number;
};

/** 每集累计实看满 10 秒才算有效观看（docs/03 第 8.1 节、第 11 节「有效观众」） */
export const EFFECTIVE_WATCH_MS = 10_000;
/**
 * 连续播放时每攒满 30 秒先报一段：页面被浏览器直接清掉（往返缓存里被淘汰、手机后台被杀）时，
 * 还没结算的那一段就丢了（见 track.ts），分段越短丢得越少；30 秒一段每位观众每分钟也只多两条事件。
 */
export const WATCH_CHUNK_MS = 30_000;

type Emit = (type: EventType, fields: TrackFields) => void;

export type PlaybackStatsOptions = {
	emit?: Emit;
	now?: () => number;
	visible?: () => boolean;
};

type Pending = { kind: 'fail'; failure: PlayFailure } | { kind: 'wall' };

export class PlaybackStats {
	private readonly src: PlaySource;
	private readonly emit: Emit;
	private readonly now: () => number;
	private readonly isVisible: () => boolean;
	private readonly onVisibility: () => void;

	private dramaId = 0;
	private episodeId = 0;
	private trial = false;
	/** 本集选源已成功 */
	private ready = false;
	/** 本集已经出过画面 */
	private started = false;
	/** 进行中的播放意图从什么时候开始；null = 没有（预加载中 / 已滑走） */
	private intentAt: number | null = null;
	/** 本次意图是否已经计过一次尝试 */
	private attempted = false;
	private pendingReport: Pending | null = null;
	private lineId = 0;

	/** 媒体处于播放状态（playing 之后、暂停 / 播完之前；缓冲等待期间仍为 true） */
	private isPlaying = false;
	private stallAt: number | null = null;
	/** 正在累计的这一段观看从什么时候开始；null = 没在累计 */
	private segmentAt: number | null = null;
	/** 本集累计实看（毫秒），用于 10 秒有效门槛 */
	private watchedMs = 0;
	/** 已累计但还没上报的观看（毫秒） */
	private unreportedMs = 0;

	constructor(src: PlaySource, options: PlaybackStatsOptions = {}) {
		this.src = src;
		this.emit = options.emit ?? track;
		this.now = options.now ?? (() => performance.now());
		this.isVisible = options.visible ?? (() => typeof document === 'undefined' || document.visibilityState !== 'hidden');
		this.onVisibility = () => this.visibility(this.isVisible());
		// 播放器在服务端渲染时也会被构造：只在浏览器里挂监听
		if (typeof document !== 'undefined') {
			document.addEventListener('visibilitychange', this.onVisibility);
		}
	}

	/**
	 * 开始一集（含首次与换集）：先结算上一集；intent 表示这一集是否马上要播。
	 * 不传 dramaId 时沿用上一集的（「重试」重新加载同一集）。
	 */
	load(ids: { dramaId?: number; episodeId: number }, intent: boolean) {
		this.settle();
		this.dramaId = ids.dramaId ?? this.dramaId;
		this.episodeId = ids.episodeId;
		this.trial = false;
		this.ready = false;
		this.started = false;
		this.intentAt = null;
		this.attempted = false;
		this.pendingReport = null;
		this.lineId = 0;
		this.stallAt = null;
		this.watchedMs = 0;
		this.unreportedMs = 0;
		if (intent) {
			this.intent();
		}
	}

	/** 用户要看这一集了：自动起播、刷剧滑到这一条 */
	intent() {
		if (!this.episodeId || this.started || this.intentAt !== null) {
			return;
		}
		this.intentAt = this.now();
		this.attempted = false;
		const pending = this.pendingReport;
		this.pendingReport = null;
		if (pending?.kind === 'fail') {
			this.attempt();
			this.error(pending.failure);
		} else if (pending?.kind === 'wall') {
			this.wallShown('locked');
		} else if (this.ready) {
			this.attempt();
		}
	}

	/** 出画面之前用户手动点了播放（多半是浏览器拦了自动播放）：首帧从这一下重新计时，不算新的尝试 */
	userPlay() {
		if (this.started || !this.episodeId) {
			return;
		}
		if (this.intentAt === null) {
			this.intent();
		} else {
			this.intentAt = this.now();
		}
	}

	/** 刷剧滑走：结算观看；还没出画面的这次意图作废（算起播前流失），滑回来重新计一次尝试 */
	idle() {
		this.isPlaying = false;
		this.stallAt = null;
		this.stopSegment();
		this.flushWatch();
		if (!this.started) {
			this.intentAt = null;
		}
	}

	/** 选源成功 */
	sourcesReady(trial: boolean) {
		this.ready = true;
		this.trial = trial;
		if (this.intentAt !== null && !this.attempted) {
			this.attempt();
		}
	}

	/**
	 * 选源被拒或失败（播放器随后显示 kind 对应的墙；status 是 HTTP 状态，网络错误为 0）：
	 * 登录 / 会员墙是观看权限、notfound（分集不可播放，多半是旧链接）是内容状态，都不算播放失败；
	 * 其余（限流、服务不可用、网络错误）算「选源」类失败。
	 */
	blocked(kind: string, status: number) {
		if (kind === 'login' || kind === 'vip') {
			this.wall(kind, false);
		} else if (kind !== 'notfound') {
			this.failed({ reason: 'source', code: status || undefined });
		}
	}

	/** 致命失败（选源失败、所有线路都不可用、解码失败等），之后播放器会显示错误墙 */
	failed(failure: PlayFailure) {
		this.isPlaying = false;
		this.stallAt = null;
		this.stopSegment();
		this.flushWatch();
		if (!this.started && this.intentAt === null) {
			this.pendingReport = { kind: 'fail', failure };
			return;
		}
		if (!this.started && !this.attempted) {
			this.attempt();
		}
		this.error(failure);
	}

	/** 观看权限墙。只有登录墙是注册墙；会员墙是付费墙，不在这个漏斗里 */
	wall(kind: 'login' | 'vip', trialEnded: boolean) {
		if (kind !== 'login') {
			return;
		}
		if (trialEnded) {
			this.wallShown('trial_end');
		} else if (this.intentAt === null && !this.started) {
			this.pendingReport = { kind: 'wall' };
		} else {
			this.wallShown('locked');
		}
	}

	/** 注册墙上点了登录 / 注册 */
	wallClick(to: 'login' | 'register') {
		this.scoped('register_wall', { step: 'click', to });
		rememberWallClick(this.dramaId, this.episodeId);
	}

	/** <video> 的 playing：第一次是起播成功，之后是暂停或卡顿后恢复 */
	playing(lineId: number) {
		if (!this.episodeId) {
			return;
		}
		this.lineId = lineId;
		const now = this.now();
		if (!this.started) {
			// 防御：没记到意图也别丢掉这次起播
			this.intent();
			if (!this.attempted) {
				this.attempt();
			}
			this.started = true;
			this.scoped('play_start', { ms: Math.max(0, Math.round(now - (this.intentAt ?? now))), line_id: lineId || undefined });
		} else if (this.stallAt !== null) {
			this.scoped('play_stall', { ms: Math.round(now - this.stallAt), line_id: lineId || undefined });
		}
		this.stallAt = null;
		this.isPlaying = true;
		this.startSegment();
	}

	/** <video> 的 pause */
	paused() {
		this.isPlaying = false;
		this.stallAt = null;
		this.stopSegment();
		this.flushWatch();
	}

	/** <video> 的 waiting：起播后、不是拖进度引起的才算卡顿；缓冲期间不计观看时长 */
	waiting(seeking: boolean) {
		if (!this.started) {
			return;
		}
		this.stopSegment();
		if (!seeking && this.isPlaying && this.stallAt === null) {
			this.stallAt = this.now();
		}
	}

	/** 播完（含试看片段播到头） */
	ended() {
		this.paused();
	}

	/**
	 * 换线路：auto = 播放器因故障自动换，否则是用户手选。
	 * 手选换线要重新挂载、缓冲，这段等待是用户自己点的，按「暂停 → 恢复」处理，不算卡顿；
	 * 故障自动换线之前多半已经在卡，等待一直算到恢复播放。
	 */
	lineSwitch(from: number, to: number, auto: boolean) {
		if (this.intentAt === null && !this.started) {
			return;
		}
		if (!auto) {
			this.paused();
		}
		this.scoped('line_switch', { from, to, auto });
	}

	/** <video> 的 timeupdate：连续播放攒满一段就先报 */
	tick() {
		if (this.segmentAt !== null && this.unreportedMs + (this.now() - this.segmentAt) >= WATCH_CHUNK_MS) {
			this.stopSegment();
			this.flushWatch();
			this.startSegment();
		}
	}

	/** 页面隐藏 / 恢复：后台播放不算观看 */
	visibility(visible: boolean) {
		if (!visible) {
			this.stopSegment();
			this.flushWatch();
		} else if (this.isPlaying && this.stallAt === null) {
			this.startSegment();
		}
	}

	/** 播放器销毁：结算并摘掉监听 */
	dispose() {
		this.settle();
		if (typeof document !== 'undefined') {
			document.removeEventListener('visibilitychange', this.onVisibility);
		}
	}

	private settle() {
		this.isPlaying = false;
		this.stopSegment();
		this.flushWatch();
	}

	private startSegment() {
		if (this.segmentAt === null && this.isVisible()) {
			this.segmentAt = this.now();
		}
	}

	private stopSegment() {
		if (this.segmentAt !== null) {
			const ms = Math.max(0, this.now() - this.segmentAt);
			this.watchedMs += ms;
			this.unreportedMs += ms;
			this.segmentAt = null;
		}
	}

	private flushWatch() {
		if (this.watchedMs < EFFECTIVE_WATCH_MS) {
			return;
		}
		const seconds = Math.floor(this.unreportedMs / 1000);
		if (seconds < 1) {
			return;
		}
		this.unreportedMs -= seconds * 1000;
		this.scoped('watch', { seconds, src: this.src });
	}

	private attempt() {
		this.attempted = true;
		this.scoped('play_attempt', { src: this.src, trial: this.trial || undefined });
	}

	private error(failure: PlayFailure) {
		this.scoped('play_error', {
			reason: failure.reason,
			stage: this.started ? 'post' : 'pre',
			code: failure.code || undefined,
			detail: failure.detail?.slice(0, 60),
			line_id: failure.lineId || this.lineId || undefined
		});
	}

	private wallShown(src: 'locked' | 'trial_end') {
		this.scoped('register_wall', { step: 'show', src });
	}

	private scoped(type: EventType, props: EventProps) {
		this.emit(type, { drama_id: this.dramaId, episode_id: this.episodeId, props });
	}
}

/** 播放器内核会调用的方法（与 PlaybackStats 的同名公开方法一一对应） */
export type PlaybackMethod =
	| 'load'
	| 'intent'
	| 'userPlay'
	| 'idle'
	| 'sourcesReady'
	| 'blocked'
	| 'failed'
	| 'wall'
	| 'wallClick'
	| 'playing'
	| 'paused'
	| 'waiting'
	| 'ended'
	| 'lineSwitch'
	| 'tick'
	| 'visibility'
	| 'dispose';

/** 模块加载完之前记下的一次调用：方法、参数、发生时刻、当时页面是否可见 */
export type PlaybackCall = [method: PlaybackMethod, args: unknown[], at: number, visible: boolean];

/**
 * 建好 PlaybackStats，并按原时刻重放模块加载完之前记下的调用（playback-lazy.ts），
 * 返回之后直接调用用的入口。重放期间时钟与可见性取记录里的值，首帧耗时与观看时长不受加载快慢影响。
 */
export function replayPlayback(
	src: PlaySource,
	calls: PlaybackCall[],
	options: Pick<PlaybackStatsOptions, 'emit'> = {}
): (method: PlaybackMethod, args: unknown[]) => void {
	let replaying: PlaybackCall | null = null;
	const stats = new PlaybackStats(src, {
		...options,
		now: () => (replaying ? replaying[2] : performance.now()),
		visible: () => (replaying ? replaying[3] : document.visibilityState !== 'hidden')
	});
	const invoke = (method: PlaybackMethod, args: unknown[]) => {
		(stats[method] as (...a: unknown[]) => void).apply(stats, args);
	};
	for (const call of calls) {
		replaying = call;
		invoke(call[0], call[1]);
	}
	replaying = null;
	return invoke;
}
