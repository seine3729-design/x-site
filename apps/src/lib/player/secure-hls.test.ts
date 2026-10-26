import type { HlsConfig, Loader, LoaderCallbacks, LoaderConfiguration, LoaderContext, LoaderStats } from 'hls.js';
import { describe, expect, it, vi } from 'vitest';
import { createSecureLoader, isApiUrl } from './secure-hls.ts';

const ORIGIN = 'https://huangguo.example';

function newStats(): LoaderStats {
	return {
		aborted: false,
		loaded: 0,
		retry: 0,
		total: 0,
		chunkCount: 0,
		bwEstimate: 0,
		loading: { start: 0, first: 0, end: 0 },
		parsing: { start: 0, end: 0 },
		buffering: { start: 0, first: 0, end: 0 }
	};
}

const loaderConfig = { loadPolicy: { maxTimeToFirstByteMs: 0, maxLoadTimeMs: 0 } } as unknown as LoaderConfiguration;

function callbacks() {
	return {
		onSuccess: vi.fn(),
		onError: vi.fn(),
		onTimeout: vi.fn(),
		onAbort: vi.fn()
	} satisfies LoaderCallbacks<LoaderContext>;
}

/** 与 hls.js BaseLoader 语义一致：abort() 回调 onAbort；destroy() 先解除回调再静默中止 */
class FakeDefaultLoader implements Partial<Loader<LoaderContext>> {
	static loads: string[] = [];
	stats = newStats();
	context: LoaderContext | null = null;
	callbacks: LoaderCallbacks<LoaderContext> | null = null;
	load(context: LoaderContext, _config: LoaderConfiguration, callbacks: LoaderCallbacks<LoaderContext>) {
		FakeDefaultLoader.loads.push(context.url);
		this.context = context;
		this.callbacks = callbacks;
	}
	abort() {
		this.callbacks?.onAbort?.(this.stats, this.context!, null);
	}
	destroy() {
		this.callbacks = null;
	}
}

function makeLoader(secureFetch: (path: string) => Promise<Response>) {
	const Loader = createSecureLoader(
		FakeDefaultLoader as unknown as new (c: HlsConfig) => Loader<LoaderContext>,
		secureFetch,
		ORIGIN,
		newStats
	);
	return new Loader({} as HlsConfig);
}

const ctx = (url: string, responseType: string) => ({ url, responseType, type: 'manifest' }) as unknown as LoaderContext;

describe('isApiUrl', () => {
	it('只把同源 /api/v1/ 地址交给加密接口', () => {
		expect(isApiUrl('/api/v1/hls/t/index.m3u8', ORIGIN)).toBe(true);
		expect(isApiUrl(`${ORIGIN}/api/v1/hls/key/1/2`, ORIGIN)).toBe(true);
		expect(isApiUrl('https://v1.cdn.example/hls/9f3c/seg-1.m4s', ORIGIN)).toBe(false);
		expect(isApiUrl(`${ORIGIN}/static/a.m3u8`, ORIGIN)).toBe(false);
	});
});

describe('SecureLoader', () => {
	it('清单走加密接口，以文本交给 hls.js', async () => {
		const secureFetch = vi.fn(async () => new Response('#EXTM3U\n#EXT-X-VERSION:7\n'));
		const cb = callbacks();
		makeLoader(secureFetch).load(ctx(`${ORIGIN}/api/v1/hls/tok/index.m3u8?lang=en`, 'text'), loaderConfig, cb);
		await vi.waitFor(() => expect(cb.onSuccess).toHaveBeenCalled());
		expect(secureFetch).toHaveBeenCalledWith('/api/v1/hls/tok/index.m3u8?lang=en', expect.anything());
		expect(cb.onSuccess.mock.calls[0]![0]).toMatchObject({ data: '#EXTM3U\n#EXT-X-VERSION:7\n', code: 200 });
	});

	it('AES-128 密钥走加密接口，以 ArrayBuffer 交给 hls.js', async () => {
		const key = new Uint8Array(16).fill(7);
		const cb = callbacks();
		makeLoader(async () => new Response(key)).load(ctx('/api/v1/hls/key/42/1?t=x', 'arraybuffer'), loaderConfig, cb);
		await vi.waitFor(() => expect(cb.onSuccess).toHaveBeenCalled());
		const data = cb.onSuccess.mock.calls[0]![0].data as ArrayBuffer;
		expect(new Uint8Array(data)).toEqual(key);
		expect(cb.onSuccess.mock.calls[0]![1].loaded).toBe(16);
	});

	it('CDN 切片交给默认加载器，不经过加密接口', () => {
		const secureFetch = vi.fn();
		FakeDefaultLoader.loads = [];
		makeLoader(secureFetch).load(ctx('https://v1.cdn.example/hls/9f3c/seg-1.m4s', 'arraybuffer'), loaderConfig, callbacks());
		expect(FakeDefaultLoader.loads).toEqual(['https://v1.cdn.example/hls/9f3c/seg-1.m4s']);
		expect(secureFetch).not.toHaveBeenCalled();
	});

	it('接口错误按 HTTP 状态回调 onError（如播放令牌过期 401）', async () => {
		const cb = callbacks();
		makeLoader(async () => new Response('{}', { status: 401 })).load(ctx('/api/v1/hls/tok/index.m3u8', 'text'), loaderConfig, cb);
		await vi.waitFor(() => expect(cb.onError).toHaveBeenCalled());
		expect(cb.onError.mock.calls[0]![0].code).toBe(401);
		expect(cb.onSuccess).not.toHaveBeenCalled();
	});

	it('abort 后不再回调成功', async () => {
		let release!: (r: Response) => void;
		const cb = callbacks();
		const loader = makeLoader(
			(_path, init?: { signal?: AbortSignal }) =>
				new Promise<Response>((resolve, reject) => {
					release = resolve;
					init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
				})
		);
		loader.load(ctx('/api/v1/hls/tok/index.m3u8', 'text'), loaderConfig, cb);
		loader.abort();
		release(new Response('#EXTM3U'));
		await new Promise((r) => setTimeout(r, 10));
		expect(cb.onAbort).toHaveBeenCalledOnce();
		expect(cb.onSuccess).not.toHaveBeenCalled();
		expect(cb.onError).not.toHaveBeenCalled();
	});

	// hls.js 的 onAbort 处理（resetLoader）里会调用 loader.destroy()：destroy 不能再回调 onAbort，否则无限递归
	it('切片加载中途中止：onAbort 里销毁加载器不递归', () => {
		const loader = makeLoader(vi.fn());
		const cb = callbacks();
		cb.onAbort.mockImplementation(() => loader.destroy());
		loader.load(ctx('https://v1.cdn.example/hls/9f3c/seg-2.m4s', 'arraybuffer'), loaderConfig, cb);
		expect(() => loader.abort()).not.toThrow();
		expect(cb.onAbort).toHaveBeenCalledOnce();
	});

	it('清单加载中途中止：onAbort 里销毁加载器不递归，之后也不回调成功', async () => {
		let release!: (r: Response) => void;
		const loader = makeLoader(
			(_path, init?: { signal?: AbortSignal }) =>
				new Promise<Response>((resolve, reject) => {
					release = resolve;
					init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
				})
		);
		const cb = callbacks();
		cb.onAbort.mockImplementation(() => loader.destroy());
		loader.load(ctx('/api/v1/hls/tok/index.m3u8', 'text'), loaderConfig, cb);
		expect(() => loader.abort()).not.toThrow();
		release(new Response('#EXTM3U'));
		await new Promise((r) => setTimeout(r, 10));
		expect(cb.onAbort).toHaveBeenCalledOnce();
		expect(cb.onSuccess).not.toHaveBeenCalled();
	});

	it('destroy 静默中止，不回调 onAbort', () => {
		const cb = callbacks();
		const loader = makeLoader(() => new Promise<Response>(() => {}));
		loader.load(ctx('/api/v1/hls/tok/index.m3u8', 'text'), loaderConfig, cb);
		loader.destroy();
		expect(cb.onAbort).not.toHaveBeenCalled();
	});
});
