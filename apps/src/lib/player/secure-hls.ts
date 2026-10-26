/**
 * 加密播放链路（docs/10 第 7 节、docs/04 C.1）。
 *
 * - 视频切片是 HLS AES-128 加密的 CMAF，直接从 CDN 拉取（hls.js 默认加载器）；
 * - 清单（/api/v1/hls/...m3u8）与内容密钥（/api/v1/hls/key/...）走加密接口，网络上只有密文；
 * - hls.js 用 MSE（iOS 17.1+ 为 ManagedMediaSource）把解密后的数据喂给 <video>，
 *   <video>.src 是浏览器生成的临时 blob: 地址，不暴露清单或切片地址。
 *
 * 不支持 MSE 的旧设备不回退到原生 HLS：原生播放器取密钥时无法走加密接口。
 */
import type {
	HlsConfig,
	Loader,
	LoaderCallbacks,
	LoaderConfiguration,
	LoaderContext,
	LoaderStats
} from 'hls.js';
import { secureApi, type SecureRequestInit } from '#lib/crypto/api-client.ts';

type SecureFetch = (path: string, init?: SecureRequestInit) => Promise<Response>;
type LoaderClass = new (config: HlsConfig) => Loader<LoaderContext>;

export class PlaybackUnsupportedError extends Error {
	constructor() {
		super('当前浏览器不支持加密播放（需要 MSE / ManagedMediaSource）');
		this.name = 'PlaybackUnsupportedError';
	}
}

/** 同源 /api/ 下的地址（清单、密钥）走加密接口；其余（CDN 切片）交给默认加载器。 */
export function isApiUrl(url: string, origin: string): boolean {
	const parsed = new URL(url, origin);
	return parsed.origin === origin && parsed.pathname.startsWith('/api/');
}

export function createSecureLoader(
	DefaultLoader: LoaderClass,
	secureFetch: SecureFetch,
	origin: string,
	newStats: () => LoaderStats
): LoaderClass {
	return class SecureLoader implements Loader<LoaderContext> {
		context: LoaderContext | null = null;
		#config: HlsConfig;
		#stats: LoaderStats = newStats();
		#delegate: Loader<LoaderContext> | null = null;
		#controller: AbortController | null = null;
		#timer: ReturnType<typeof setTimeout> | undefined;
		#callbacks: LoaderCallbacks<LoaderContext> | null = null;

		constructor(config: HlsConfig) {
			this.#config = config;
		}

		get stats(): LoaderStats {
			return this.#delegate?.stats ?? this.#stats;
		}

		set stats(value: LoaderStats) {
			this.#stats = value;
		}

		load(context: LoaderContext, config: LoaderConfiguration, callbacks: LoaderCallbacks<LoaderContext>): void {
			this.context = context;
			if (!isApiUrl(context.url, origin)) {
				this.#delegate = new DefaultLoader(this.#config);
				this.#delegate.load(context, config, callbacks);
				return;
			}
			this.#callbacks = callbacks;
			const stats = this.#stats;
			const controller = new AbortController();
			this.#controller = controller;
			stats.loading.start = performance.now();

			const limit = config.loadPolicy.maxLoadTimeMs;
			if (Number.isFinite(limit) && limit > 0) {
				this.#timer = setTimeout(() => {
					controller.abort();
					callbacks.onTimeout(stats, context, null);
				}, limit);
			}

			const url = new URL(context.url, origin);
			secureFetch(url.pathname + url.search, { signal: controller.signal, headers: context.headers })
				.then(async (response) => {
					stats.loading.first = Math.max(performance.now(), stats.loading.start);
					if (!response.ok) {
						clearTimeout(this.#timer);
						callbacks.onError({ code: response.status, text: response.statusText }, context, response, stats);
						return;
					}
					const data = context.responseType === 'arraybuffer' ? await response.arrayBuffer() : await response.text();
					clearTimeout(this.#timer);
					if (controller.signal.aborted) {
						return; // 读响应体期间被中止或销毁
					}
					stats.loaded = stats.total = typeof data === 'string' ? data.length : data.byteLength;
					stats.loading.end = Math.max(performance.now(), stats.loading.first);
					callbacks.onSuccess({ url: context.url, data, code: response.status }, stats, context, response);
				})
				.catch((error: unknown) => {
					clearTimeout(this.#timer);
					if (controller.signal.aborted) {
						return; // abort() / 超时已经通知过 hls.js
					}
					callbacks.onError({ code: 0, text: error instanceof Error ? error.message : String(error) }, context, null, stats);
				});
		}

		/** 中止并回调 onAbort（与 hls.js 自带加载器一致）。 */
		abort(): void {
			clearTimeout(this.#timer);
			if (this.#delegate) {
				this.#delegate.abort();
				return;
			}
			if (this.#controller && !this.#controller.signal.aborted) {
				this.#controller.abort();
				this.#stats.aborted = true;
				const callbacks = this.#callbacks;
				if (this.context) {
					callbacks?.onAbort?.(this.#stats, this.context, null);
				}
			}
		}

		/**
		 * 静默销毁：先解除回调再中止，不回调 onAbort。
		 * hls.js 的 onAbort 处理里会调用 loader.destroy()，这里若再走 abort() 会无限递归。
		 */
		destroy(): void {
			clearTimeout(this.#timer);
			this.#callbacks = null;
			const delegate = this.#delegate;
			this.#delegate = null;
			delegate?.destroy();
			if (this.#controller && !this.#controller.signal.aborted) {
				this.#controller.abort();
			}
			this.#controller = null;
			this.context = null;
		}
	};
}

/**
 * 把加密 HLS 挂到 <video> 上。返回 hls 实例，离开页面时调用 destroy()（会释放 blob: 地址）。
 * manifestPath 是选源接口返回的清单地址（同源 /api/v1/hls/...）。
 */
export async function attachSecureHls(
	video: HTMLVideoElement,
	manifestPath: string,
	options: { secureFetch?: SecureFetch; hlsConfig?: Partial<HlsConfig> } = {}
) {
	const { default: Hls, LoadStats } = await import('hls.js');
	if (!Hls.isSupported()) {
		throw new PlaybackUnsupportedError();
	}
	const loader = createSecureLoader(
		Hls.DefaultConfig.loader as LoaderClass,
		options.secureFetch ?? secureApi.secureFetch,
		location.origin,
		() => new LoadStats()
	);
	const hls = new Hls({ ...options.hlsConfig, loader });
	hls.attachMedia(video);
	hls.loadSource(manifestPath);
	return hls;
}
