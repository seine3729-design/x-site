/**
 * 播放器内核（docs/04 C.1、B.6）：选源 → 加密 HLS（hls.js 动态导入，MSE / ManagedMediaSource）→ 播放状态。
 *
 * - 清单与密钥经加密接口（#lib/player/secure-hls.ts），切片从线路 CDN 直取；<video> 地址是临时 blob:。
 * - 观看权限：401 登录墙、402 VIP 墙；试看令牌的清单只含前 trial_sec 秒，播到头显示对应的墙。
 * - 线路：自动模式用第一条，致命网络错误按顺序换下一条并从原位置续播；手选线路记在本机。
 * - 令牌过期（401/403）时重新选源，从原位置续播，不打断用户；连续两次仍被拒就显示错误墙，不无限重试。
 * - 播放令牌绑定选源时的 X-Device-Id：清单与密钥请求用 deviceSecureFetch 带同一个标识。
 */
import type Hls from 'hls.js';
import type { ErrorData, Level } from 'hls.js';
import { lazyPlaybackStats, type PlaybackHook } from '#lib/analytics/playback-lazy.ts';
import type { PlayFailure, PlaySource } from '#lib/analytics/playback.ts';
import type { EpisodeAccess, PlaybackLine, PlaybackSources } from '#lib/api/types.ts';
import { attachSecureHls, PlaybackUnsupportedError } from './secure-hls.ts';

export type Wall = 'login' | 'vip' | 'unavailable' | 'unsupported' | 'notfound' | 'ratelimited' | 'error';
export type Phase = 'idle' | 'loading' | 'ready' | 'playing' | 'paused' | 'ended' | 'blocked';

const MANUAL_LINE_KEY = 'xs_line_manual';
const SPEEDS = [0.75, 1, 1.25, 1.5, 2] as const;
export { SPEEDS };

export type QualityOption = { index: number; label: string };

/** 线路质量上报（POST /api/v1/lines/report 的一条） */
export type LineReport = {
	line_id: number;
	event: 'first_frame' | 'stall' | 'error' | 'switch';
	ms?: number;
	reason?: 'timeout' | 'http_4xx' | 'http_5xx' | 'cors' | 'decode';
	to_line_id?: number;
};

function manualLine(): number | null {
	try {
		const value = Number(localStorage.getItem(MANUAL_LINE_KEY));
		return Number.isSafeInteger(value) && value > 0 ? value : null;
	} catch {
		return null;
	}
}

function errorStatus(err: unknown): { status: number; code: string } {
	if (err && typeof err === 'object' && 'status' in err && 'code' in err) {
		return { status: Number((err as { status: unknown }).status), code: String((err as { code: unknown }).code) };
	}
	return { status: 0, code: 'NETWORK' };
}

/** 短边作为档位名：竖屏 720×1280 也叫 720P */
function levelLabel(level: Level): string {
	const side = Math.min(level.width || level.height, level.height || level.width);
	return side > 0 ? `${side}P` : `${Math.round(level.bitrate / 1000)}K`;
}

export class PlayerController {
	phase = $state<Phase>('idle');
	wall = $state<Wall | null>(null);
	access = $state<EpisodeAccess>('free');
	trialSec = $state(0);
	/** 试看结束（清单只含试看片段，播到头） */
	trialEnded = $state(false);

	currentTime = $state(0);
	duration = $state(0);
	/**
	 * 整集时长（秒），取自选源的 duration_sec。试看清单只含前 trial_sec 秒，video.duration（duration）只有试看长度，
	 * 进度条若按它显示就只有试看秒数；用 fullDuration 把总长显示为整集，可播区间仍只到 duration（试看片段）。
	 * 为 0 表示老产物未回填，前台回退到 duration（与改动前一致）。
	 */
	fullDuration = $state(0);
	buffered = $state(0);
	muted = $state(false);
	volume = $state(1);
	rate = $state(1);
	/** 浏览器不允许有声自动播放，退回静音播放时为 true（提示「点画面开声音」） */
	autoMuted = $state(false);
	waiting = $state(false);

	lines = $state<PlaybackLine[]>([]);
	lineIndex = $state(0);
	qualities = $state<QualityOption[]>([]);
	/** -1 = 自动 */
	quality = $state(-1);
	activeQualityLabel = $state('');

	/** 画面宽高比（宽 / 高），竖屏默认 9:16；拿到视频元数据后更新，电脑端据此调整画框 */
	aspect = $state(9 / 16);

	/**
	 * 清单解析完是否自动播放。刷剧流里上下两条要「挂上 hls.js、缓冲好首段但不播」，
	 * 滑到它时再 activate()，这样切换不用等选源 + 清单 + 首个切片（docs/04 B.7）。
	 */
	autoplay = true;
	/** 预加载时的缓冲上限（秒）：只要够瞬间起播，不占太多内存；成为当前条后调回正常值 */
	#bufferTarget = 0;

	/**
	 * 最近一次表达的播放意图：play() 置 true、pause() 置 false。
	 * video.play() 返回的 promise 若被随后的 pause() 打断会 reject，catch 里据此决定是否静音重试——
	 * 用户已点暂停时就不再把画面重新播起来，修复连点 / 竞态下「点暂停没反应」。
	 */
	#wantsPlay = false;

	#video: HTMLVideoElement | null = null;
	#videoReady: Promise<HTMLVideoElement>;
	#resolveVideo: (video: HTMLVideoElement) => void = () => {};
	#hls: Hls | null = null;
	#episodeId = 0;
	#generation = 0;
	#startAt = 0;
	#loadedAt = 0;
	/** 本集已经试过的线路（内部记录，不参与渲染） */
	#triedLines: number[] = [];
	#mediaRecoveries = 0;
	/** 令牌过期重新选源的次数：开始播放后清零，连续两次仍被拒就停下来提示，不再无限重试 */
	#tokenRetries = 0;
	#onLineReport: ((event: LineReport) => void) | null = null;
	/**
	 * 埋点（W4-1）：传了 analytics 才有；只在各个时刻通知它，口径换算都在 #lib/analytics/playback.ts
	 * （动态导入，不进首屏包，见 playback-lazy.ts）
	 */
	#stats: PlaybackHook | null = null;

	constructor(
		options: {
			onLineReport?: (event: LineReport) => void;
			autoplay?: boolean;
			preloadBufferSec?: number;
			/** 开启播放埋点并标明来源：play 播放页、feed 刷剧流 */
			analytics?: PlaySource;
		} = {}
	) {
		this.#onLineReport = options.onLineReport ?? null;
		this.autoplay = options.autoplay ?? true;
		this.#bufferTarget = options.preloadBufferSec ?? 0;
		this.#stats = options.analytics ? lazyPlaybackStats(options.analytics) : null;
		this.#videoReady = new Promise((resolve) => {
			this.#resolveVideo = resolve;
		});
	}

	bind(video: HTMLVideoElement) {
		this.#video = video;
		this.#resolveVideo(video);
		const sync = () => {
			this.currentTime = video.currentTime;
			this.duration = Number.isFinite(video.duration) ? video.duration : 0;
			const ranges = video.buffered;
			this.buffered = ranges.length > 0 ? ranges.end(ranges.length - 1) : 0;
			if (video.videoWidth > 0 && video.videoHeight > 0) {
				this.aspect = video.videoWidth / video.videoHeight;
			}
		};
		const handlers: [keyof HTMLVideoElementEventMap, () => void][] = [
			['timeupdate', sync],
			['timeupdate', () => this.#stats?.('tick')],
			['durationchange', sync],
			['progress', sync],
			['loadedmetadata', sync],
			['play', () => (this.phase = 'playing')],
			['playing', () => {
				this.waiting = false;
				this.phase = 'playing';
				this.#tokenRetries = 0;
				this.#stats?.('playing', this.lines[this.lineIndex]?.line_id ?? 0);
				if (this.#loadedAt > 0) {
					const line = this.lines[this.lineIndex];
					if (line) {
						this.#onLineReport?.({ line_id: line.line_id, event: 'first_frame', ms: Math.round(performance.now() - this.#loadedAt) });
					}
					this.#loadedAt = 0;
				}
			}],
			['pause', () => {
				this.#stats?.('paused');
				if (this.phase !== 'ended' && this.phase !== 'blocked') {
					this.phase = 'paused';
				}
			}],
			['waiting', () => {
				this.waiting = true;
				this.#stats?.('waiting', video.seeking);
			}],
			['canplay', () => (this.waiting = false)],
			['volumechange', () => {
				this.muted = video.muted;
				this.volume = video.volume;
				if (!video.muted) {
					this.autoMuted = false;
				}
			}],
			['ratechange', () => (this.rate = video.playbackRate)],
			['ended', () => this.#onEnded()]
		];
		for (const [name, handler] of handlers) {
			video.addEventListener(name, handler);
		}
		return () => {
			for (const [name, handler] of handlers) {
				video.removeEventListener(name, handler);
			}
		};
	}

	/**
	 * 当前已加载的分集。页面换集时 data 先变、`load()` 可能稍后才调用（要先读本机续播位置），
	 * 这段时间 `phase` 还停在上一集的 'ended'，调用方要靠它区分「真的播完了」与「刚换集」。
	 */
	get episodeId(): number {
		return this.#episodeId;
	}

	/**
	 * 播放某一集；startAt 为续播位置（秒）。context.dramaId 只用于埋点归属：
	 * 播放页换到另一部剧时组件不重建，所以随每次 load 传入（不传沿用上一次的）。
	 */
	load(episodeId: number, startAt = 0, context: { dramaId?: number } = {}): Promise<void> {
		this.#stats?.('load', { dramaId: context.dramaId, episodeId }, this.autoplay);
		return this.#load(episodeId, startAt);
	}

	/** 选源并挂载。令牌过期后重新选源也走这里：那是同一次播放的延续，不算新的播放尝试 */
	async #load(episodeId: number, startAt: number) {
		if (episodeId !== this.#episodeId) {
			this.#tokenRetries = 0;
		}
		const generation = ++this.#generation;
		this.#episodeId = episodeId;
		this.#startAt = startAt;
		this.#triedLines = [];
		this.#mediaRecoveries = 0;
		this.wall = null;
		this.trialEnded = false;
		this.phase = 'loading';
		this.currentTime = startAt;
		this.duration = 0;
		this.fullDuration = 0;
		this.qualities = [];
		this.quality = -1;
		this.destroyHls();

		const sources = await this.#fetchSources(episodeId, manualLine(), generation);
		if (generation !== this.#generation || !sources) {
			return;
		}
		this.access = sources.access;
		this.trialSec = sources.trial_sec;
		this.fullDuration = sources.duration_sec;
		this.lines = sources.lines;
		this.lineIndex = 0;
		this.#stats?.('sourcesReady', sources.trial_sec > 0);
		await this.#attach(generation);
	}

	async #fetchSources(episodeId: number, lineId: number | null, generation: number): Promise<PlaybackSources | null> {
		try {
			const { api } = await import('#lib/api/client.ts');
			const query = lineId ? `?line_id=${lineId}` : '';
			return await api.get<PlaybackSources>(`/api/v1/episodes/${episodeId}/sources${query}`);
		} catch (err) {
			const { status, code } = errorStatus(err);
			const wall: Wall =
				status === 401 || code === 'NEED_LOGIN'
					? 'login'
					: status === 402 || code === 'NEED_VIP'
						? 'vip'
						: status === 404
							? 'notfound'
							: status === 429
								? 'ratelimited'
								: status === 503
									? 'unavailable'
									: 'error';
			this.phase = 'blocked';
			this.wall = wall;
			// 埋点按墙区分：登录 / 会员墙、分集不可播放不算播放失败（见 playback.ts 的 blocked）
			if (generation === this.#generation) {
				this.#stats?.('blocked', wall, status);
			}
			return null;
		}
	}

	async #attach(generation: number) {
		// 页面可能先于播放器组件挂载就开始选源：等 <video> 绑定好再挂 hls.js
		const video = this.#video ?? (await this.#videoReady);
		if (generation !== this.#generation) {
			return;
		}
		const line = this.lines[this.lineIndex];
		if (!video || !line) {
			this.#block('unavailable', { reason: 'source', detail: 'no_line' });
			return;
		}
		if (!this.#triedLines.includes(line.line_id)) {
			this.#triedLines.push(line.line_id);
		}
		const mobile = matchMedia('(max-width: 1023px)').matches;
		try {
			const [{ default: HlsClass }, { deviceSecureFetch }] = await Promise.all([
				import('hls.js'),
				import('#lib/api/client.ts')
			]);
			const hls = await attachSecureHls(video, line.url, {
				// 播放令牌绑定选源时的设备标识：清单与密钥请求带同一个 X-Device-Id
				secureFetch: deviceSecureFetch,
				hlsConfig: {
					startPosition: this.#startAt > 0 ? this.#startAt : -1,
					// 起播档先出画面，ABR 再升档（docs/08 第 2.3 节）
					startLevel: 0,
					capLevelToPlayerSize: true,
					// 当前条按正常缓冲；预加载的那几条压到 #bufferTarget，滑过去时 activate() 再调回来
					maxBufferLength: !this.autoplay && this.#bufferTarget > 0 ? this.#bufferTarget : mobile ? 15 : 30,
					backBufferLength: 10,
					enableWorker: true
				}
			});
			if (generation !== this.#generation) {
				hls.destroy();
				return;
			}
			this.#hls = hls;
			this.#loadedAt = performance.now();
			hls.on(HlsClass.Events.MANIFEST_PARSED, () => {
				this.qualities = hls.levels
					.map((level, index) => ({ index, label: levelLabel(level), height: Math.min(level.width, level.height) }))
					.sort((a, b) => b.height - a.height)
					.map(({ index, label }) => ({ index, label }));
				this.phase = 'ready';
				if (this.autoplay) {
					void this.play();
				}
			});
			hls.on(HlsClass.Events.LEVEL_SWITCHED, (_event, data) => {
				const level = hls.levels[data.level];
				this.activeQualityLabel = level ? levelLabel(level) : '';
			});
			hls.on(HlsClass.Events.ERROR, (_event, data) => this.#onHlsError(data, generation, HlsClass));
		} catch (err) {
			const unsupported = err instanceof PlaybackUnsupportedError;
			this.#block(unsupported ? 'unsupported' : 'error', {
				reason: unsupported ? 'decode' : 'network',
				detail: unsupported ? 'unsupported' : 'attach',
				lineId: line.line_id
			});
		}
	}

	#onHlsError(data: ErrorData, generation: number, HlsClass: typeof Hls) {
		if (generation !== this.#generation || !data.fatal) {
			return;
		}
		const line = this.lines[this.lineIndex];
		if (data.type === HlsClass.ErrorTypes.MEDIA_ERROR && this.#mediaRecoveries < 2) {
			this.#mediaRecoveries += 1;
			this.#hls?.recoverMediaError();
			return;
		}
		const code = (data.response as { code?: number } | undefined)?.code ?? 0;
		if (line) {
			this.#onLineReport?.({
				line_id: line.line_id,
				event: 'error',
				reason: data.type === HlsClass.ErrorTypes.MEDIA_ERROR ? 'decode' : code >= 500 ? 'http_5xx' : code >= 400 ? 'http_4xx' : 'timeout'
			});
		}
		const resumeAt = this.#video?.currentTime ?? this.#startAt;
		// 令牌过期：重新选源（同一线路），从原位置续播；连续两次仍被拒（多半不是过期）就停下提示，
		// 避免无限重试把选源接口打到限流
		if (code === 401 || code === 403) {
			if (this.#tokenRetries < 2) {
				this.#tokenRetries += 1;
				void this.#load(this.#episodeId, resumeAt);
			} else {
				this.#block('error', { reason: 'source', code, detail: data.details, lineId: line?.line_id });
			}
			return;
		}
		// 其余网络错误：换下一条没试过的线路
		const next = this.lines.findIndex((l) => !this.#triedLines.includes(l.line_id));
		if (data.type === HlsClass.ErrorTypes.NETWORK_ERROR && next >= 0) {
			if (line) {
				this.#onLineReport?.({ line_id: line.line_id, event: 'switch', to_line_id: this.lines[next]!.line_id });
				this.#stats?.('lineSwitch', line.line_id, this.lines[next]!.line_id, true);
			}
			this.destroyHls();
			this.lineIndex = next;
			this.#startAt = resumeAt;
			void this.#attach(this.#generation);
			return;
		}
		const network = data.type === HlsClass.ErrorTypes.NETWORK_ERROR;
		this.#block(network ? 'unavailable' : 'error', {
			reason: network ? 'network' : 'decode',
			code: code || undefined,
			detail: data.details,
			lineId: line?.line_id
		});
	}

	/** 显示错误墙；failure 是给埋点的失败原因（三分类见 #lib/analytics/playback.ts） */
	#block(wall: Wall, failure?: PlayFailure) {
		this.#wantsPlay = false;
		this.destroyHls();
		this.phase = 'blocked';
		this.wall = wall;
		if (failure) {
			this.#stats?.('failed', failure);
		}
	}

	#onEnded() {
		this.#stats?.('ended');
		// 试看清单只含前 trial_sec 秒：播到头就是试看结束，按分集权限显示登录墙或 VIP 墙
		if (this.trialSec > 0) {
			const wall = this.access === 'vip' ? 'vip' : 'login';
			this.trialEnded = true;
			this.phase = 'blocked';
			this.wall = wall;
			this.#stats?.('wall', wall, true);
			return;
		}
		this.phase = 'ended';
	}

	/**
	 * 从预加载状态转为当前条：把缓冲上限调回正常值并开始播放。
	 * 清单还没解析完时只置 autoplay，解析完会自动起播。
	 */
	activate() {
		this.autoplay = true;
		this.#stats?.('intent');
		const hls = this.#hls;
		if (hls && this.#bufferTarget > 0) {
			hls.config.maxBufferLength = matchMedia('(max-width: 1023px)').matches ? 15 : 30;
		}
		if (this.phase === 'ready' || this.phase === 'paused' || this.phase === 'playing') {
			void this.play();
		}
	}

	/** 退出当前条（刷剧流滑走）：暂停但保留缓冲，滑回来可以立刻继续 */
	deactivate() {
		this.autoplay = false;
		this.pause();
		this.#stats?.('idle');
	}

	async play() {
		const video = this.#video;
		if (!video) {
			return;
		}
		this.#wantsPlay = true;
		try {
			await video.play();
		} catch {
			// play() 的 promise 可能被随后的 pause() 打断，也可能是浏览器不允许有声自动播放。
			// 若此间用户已点了暂停（#wantsPlay 变 false），尊重它，不要静音重试把画面重新播起来。
			if (!this.#wantsPlay) {
				return;
			}
			if (!video.muted) {
				video.muted = true;
				this.autoMuted = true;
				try {
					await video.play();
				} catch {
					if (this.#wantsPlay) {
						this.phase = 'paused';
					}
				}
			} else if (this.#wantsPlay) {
				this.phase = 'paused';
			}
		}
	}

	toggle() {
		const video = this.#video;
		if (!video || this.phase === 'blocked' || this.phase === 'loading') {
			return;
		}
		if (video.paused || video.ended) {
			if (this.autoMuted) {
				video.muted = false;
			}
			this.#stats?.('userPlay');
			void this.play();
		} else {
			this.pause();
		}
	}

	pause() {
		this.#wantsPlay = false;
		this.#video?.pause();
	}

	seek(seconds: number) {
		const video = this.#video;
		if (!video || !Number.isFinite(seconds)) {
			return;
		}
		video.currentTime = Math.max(0, Math.min(seconds, (video.duration || seconds) - 0.1));
		this.currentTime = video.currentTime;
	}

	seekBy(delta: number) {
		this.seek((this.#video?.currentTime ?? 0) + delta);
	}

	setMuted(value: boolean) {
		if (this.#video) {
			this.#video.muted = value;
		}
	}

	setVolume(value: number) {
		const video = this.#video;
		if (video) {
			video.volume = Math.max(0, Math.min(1, value));
			video.muted = video.volume === 0;
		}
	}

	setRate(value: number) {
		if (this.#video) {
			this.#video.playbackRate = value;
		}
	}

	setQuality(index: number) {
		if (this.#hls) {
			this.#hls.currentLevel = index;
			this.quality = index;
		}
	}

	/** 手动换线路：记在本机，之后选源都带上 line_id（后端在候选中且健康时置顶） */
	async setLine(index: number) {
		const line = this.lines[index];
		if (!line || index === this.lineIndex) {
			return;
		}
		try {
			localStorage.setItem(MANUAL_LINE_KEY, String(line.line_id));
		} catch {
			// 本次仍然切换
		}
		const previous = this.lines[this.lineIndex];
		if (previous) {
			this.#onLineReport?.({ line_id: previous.line_id, event: 'switch', to_line_id: line.line_id });
			this.#stats?.('lineSwitch', previous.line_id, line.line_id, false);
		}
		this.#startAt = this.#video?.currentTime ?? 0;
		this.destroyHls();
		this.lineIndex = index;
		this.phase = 'loading';
		await this.#attach(++this.#generation);
	}

	/** 「刷新当前线路」：重新选源后从原位置续播（用户主动重试，算一次新的播放尝试） */
	retry() {
		void this.load(this.#episodeId, this.#video?.currentTime || this.#startAt);
	}

	/** 播放埋点入口（没开 analytics 时为 null），给播放器界面上报注册墙点击等：stats?.('wallClick', 'register') */
	get stats(): PlaybackHook | null {
		return this.#stats;
	}

	destroyHls() {
		this.#hls?.destroy();
		this.#hls = null;
	}

	destroy() {
		this.#generation += 1;
		this.#stats?.('dispose');
		this.destroyHls();
		this.#video = null;
	}
}
