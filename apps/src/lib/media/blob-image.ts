/**
 * 图片以临时 blob: 地址显示（docs/04 C.8）：不加密，只是不把 CDN 原始地址写进 <img src>。
 * 图片本身照常走 CDN 与浏览器 HTTP 缓存，重复显示同一张图不会重复下载。
 *
 * 图片线路需要对站点域名返回 CORS 头（fetch 读取响应体需要），CSP 的 connect-src 需包含图片域名。
 */
import type { FetchLike } from '#lib/crypto/api-client.ts';

export class ImageLoadError extends Error {
	constructor(readonly sources: readonly string[]) {
		super('所有图片线路都加载失败');
		this.name = 'ImageLoadError';
	}
}

export type BlobImageOptions = {
	signal?: AbortSignal;
	/** 首屏图片传 high，与 <img fetchpriority> 同义 */
	priority?: RequestPriority;
	fetch?: FetchLike;
	createObjectURL?: (blob: Blob) => string;
};

/**
 * 按顺序尝试候选地址（同一张图在不同图片线路上的地址，docs/12 第 6 节），返回第一张成功的 blob: 地址。
 * 调用方负责在不再显示时 URL.revokeObjectURL。
 */
export async function loadBlobUrl(sources: readonly string[], options: BlobImageOptions = {}): Promise<string> {
	const doFetch = options.fetch ?? ((input, init) => fetch(input, init));
	const createObjectURL = options.createObjectURL ?? ((blob: Blob) => URL.createObjectURL(blob));
	for (const src of sources) {
		options.signal?.throwIfAborted();
		try {
			const response = await doFetch(src, {
				mode: 'cors',
				credentials: 'omit',
				signal: options.signal,
				priority: options.priority ?? 'auto'
			});
			if (!response.ok) {
				continue;
			}
			const blob = await response.blob();
			// 只接受图片，防止线路被劫持后返回 HTML/脚本
			if (!blob.type.startsWith('image/') || blob.type === 'image/svg+xml') {
				continue;
			}
			return createObjectURL(blob);
		} catch (error) {
			if (options.signal?.aborted) {
				throw error;
			}
			// 网络错误：换下一条线路
		}
	}
	throw new ImageLoadError(sources);
}
