import { describe, expect, it, vi } from 'vitest';
import type { FetchLike } from '#lib/crypto/api-client.ts';
import { ImageLoadError, loadBlobUrl } from './blob-image.ts';

const png = () => new Response(new Blob([Uint8Array.of(0x89, 0x50, 0x4e, 0x47)], { type: 'image/png' }));

function fakeFetch(routes: Record<string, () => Response | Promise<Response>>) {
	const calls: { url: string; init?: RequestInit }[] = [];
	const impl: FetchLike = async (input, init) => {
		const url = String(input);
		calls.push({ url, init });
		const route = routes[url];
		if (!route) throw new TypeError('network error');
		return route();
	};
	return { impl, calls };
}

describe('loadBlobUrl', () => {
	it('返回 blob: 地址，不带凭据、以 CORS 模式请求', async () => {
		const { impl, calls } = fakeFetch({ 'https://img1.example/c/1.webp': png });
		const url = await loadBlobUrl(['https://img1.example/c/1.webp'], { fetch: impl, priority: 'high' });
		expect(url).toMatch(/^blob:/);
		expect(calls[0]!.init).toMatchObject({ mode: 'cors', credentials: 'omit', priority: 'high' });
		URL.revokeObjectURL(url);
	});

	it('首选线路失败（网络错误或 4xx/5xx）时按顺序换下一条', async () => {
		const { impl, calls } = fakeFetch({
			'https://img2.example/c/1.webp': () => new Response('', { status: 503 }),
			'https://img3.example/c/1.webp': png
		});
		const createObjectURL = vi.fn(() => 'blob:https://site.example/uuid');
		const url = await loadBlobUrl(
			['https://img1.example/c/1.webp', 'https://img2.example/c/1.webp', 'https://img3.example/c/1.webp'],
			{ fetch: impl, createObjectURL }
		);
		expect(url).toBe('blob:https://site.example/uuid');
		expect(calls.map((c) => c.url)).toEqual([
			'https://img1.example/c/1.webp',
			'https://img2.example/c/1.webp',
			'https://img3.example/c/1.webp'
		]);
	});

	it('拒绝非图片与 SVG（线路被劫持返回 HTML 或脚本时不显示）', async () => {
		const { impl } = fakeFetch({
			'https://a.example/x': () => new Response('<script>', { headers: { 'content-type': 'text/html' } }),
			'https://b.example/x': () => new Response(new Blob(['<svg/>'], { type: 'image/svg+xml' }))
		});
		await expect(loadBlobUrl(['https://a.example/x', 'https://b.example/x'], { fetch: impl })).rejects.toBeInstanceOf(
			ImageLoadError
		);
	});

	it('取消后不再尝试后续线路', async () => {
		const controller = new AbortController();
		const { impl, calls } = fakeFetch({
			'https://a.example/x': () => {
				controller.abort();
				throw new DOMException('aborted', 'AbortError');
			},
			'https://b.example/x': png
		});
		await expect(
			loadBlobUrl(['https://a.example/x', 'https://b.example/x'], { fetch: impl, signal: controller.signal })
		).rejects.toThrow();
		expect(calls).toHaveLength(1);
	});
});
