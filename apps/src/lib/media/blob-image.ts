/**
 * 图片以临时 blob: 地址显示（docs/04 C.8）：不加密，只是不把 CDN 原始地址写进 <img src>。
 * 图片本身照常走 CDN 与浏览器 HTTP 缓存，重复显示同一张图不会重复下载。
 *
 * 地址可能是同源的缩放入口（`/cdn-cgi/image/…`，见 image.ts），也可能是线路上的原图。
 * 原图线路需要对站点域名返回 CORS 头（fetch 读取响应体需要），CSP 的 connect-src 需包含图片域名。
 */
import type { FetchLike } from '#lib/crypto/api-client.ts';
import { RESIZE_PATH } from './image.ts';

export class ImageLoadError extends Error {
	constructor(readonly sources: readonly string[]) {
		super('所有图片线路都加载失败');
		this.name = 'ImageLoadError';
	}
}

/** 线路明确拒绝（非 2xx 或不是图片）；和网络错误区分开：前者说明这个入口确实不能用 */
class ImageRejectedError extends Error {
	constructor(readonly status: number) {
		super(`图片地址不可用（${status}）`);
		this.name = 'ImageRejectedError';
	}
}

/**
 * 首页同一张封面常出现好几次（轮播、榜单、猜你喜欢是同一批剧），而且往往同时进入可视区：
 * 正在下载的请求合并成一个，下载完的留最近用过的若干张，不依赖浏览器 HTTP 缓存恰好命中的时机。
 *
 * 请求发出后不随某一个组件卸载而中止：别的组件可能还在等它，下完的也能被后面复用。
 * 缩放之后单张只有几十 KB，多下完一张的代价很小。
 */
export class ImageCache {
	/**
	 * 缩放入口在本页返回过非 2xx 或非图片（站点挂在没接 Cloudflare 的域名上时 `/cdn-cgi/image/` 是 404）：
	 * 之后直接用原图，不再每张图都白跑一趟。
	 */
	resizeBroken = false;
	readonly #limit: number;
	readonly #entries = new Map<string, Promise<Blob>>();

	constructor(limit = 48) {
		this.#limit = limit;
	}

	get(url: string, load: () => Promise<Blob>, signal?: AbortSignal): Promise<Blob> {
		signal?.throwIfAborted();
		let pending = this.#entries.get(url);
		if (pending) {
			// Map 按插入顺序排：删掉重放就是「最近用过」
			this.#entries.delete(url);
		} else {
			const own = load();
			pending = own;
			// 失败的不留，下次重新请求（可能只是一时的网络错误）
			own.catch(() => {
				if (this.#entries.get(url) === own) {
					this.#entries.delete(url);
				}
			});
		}
		this.#entries.set(url, pending);
		while (this.#entries.size > this.#limit) {
			const oldest = this.#entries.keys().next().value;
			if (oldest === undefined) {
				break;
			}
			this.#entries.delete(oldest);
		}
		return signal ? untilAborted(pending, signal) : pending;
	}
}

/** 全页共用的一份 */
export const imageCache = new ImageCache();

function untilAborted<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
	return new Promise<T>((resolve, reject) => {
		const onAbort = () => reject(signal.reason);
		signal.addEventListener('abort', onAbort, { once: true });
		promise.then(
			(value) => {
				signal.removeEventListener('abort', onAbort);
				resolve(value);
			},
			(error: unknown) => {
				signal.removeEventListener('abort', onAbort);
				reject(error);
			}
		);
	});
}

async function fetchImage(doFetch: FetchLike, src: string, priority: RequestPriority): Promise<Blob> {
	const response = await doFetch(src, { mode: 'cors', credentials: 'omit', priority });
	if (!response.ok) {
		throw new ImageRejectedError(response.status);
	}
	const blob = await response.blob();
	// 只接受图片，防止线路被劫持后返回 HTML/脚本
	if (!blob.type.startsWith('image/') || blob.type === 'image/svg+xml') {
		throw new ImageRejectedError(response.status);
	}
	return blob;
}

export type BlobImageOptions = {
	signal?: AbortSignal;
	/** 首屏图片传 high，与 <img fetchpriority> 同义 */
	priority?: RequestPriority;
	fetch?: FetchLike;
	createObjectURL?: (blob: Blob) => string;
	/** 默认用全页共用的 imageCache；注入了 fetch（测试）时每次一份新的，互不干扰 */
	cache?: ImageCache;
};

/**
 * 按顺序尝试候选地址（缩放后的同源地址在前，其后是同一张图在不同图片线路上的原图，docs/12 第 6 节），
 * 返回第一张成功的 blob: 地址。调用方负责在不再显示时 URL.revokeObjectURL。
 */
export async function loadBlobUrl(sources: readonly string[], options: BlobImageOptions = {}): Promise<string> {
	const doFetch = options.fetch ?? ((input, init) => fetch(input, init));
	const createObjectURL = options.createObjectURL ?? ((blob: Blob) => URL.createObjectURL(blob));
	const cache = options.cache ?? (options.fetch ? new ImageCache() : imageCache);
	const priority = options.priority ?? 'auto';
	for (const src of sources) {
		options.signal?.throwIfAborted();
		try {
			const blob = await cache.get(src, () => fetchImage(doFetch, src, priority), options.signal);
			return createObjectURL(blob);
		} catch (error) {
			if (options.signal?.aborted) {
				throw error;
			}
			if (error instanceof ImageRejectedError && src.startsWith(`${RESIZE_PATH}/`)) {
				cache.resizeBroken = true;
			}
			// 网络错误或被拒：换下一条线路
		}
	}
	throw new ImageLoadError(sources);
}
