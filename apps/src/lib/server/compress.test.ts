import { brotliDecompressSync, gunzipSync } from 'node:zlib';
import { describe, expect, it } from 'vitest';
import { chooseEncoding, compressResponse } from './compress.ts';

const html = `<!doctype html><html><body>${'<p>短剧</p>'.repeat(400)}</body></html>`;
const page = (body: string, headers: Record<string, string> = {}) =>
	new Response(body, { status: 200, headers: { 'content-type': 'text/html; charset=utf-8', ...headers } });

async function bytes(res: Response) {
	return new Uint8Array(await res.arrayBuffer());
}

describe('chooseEncoding', () => {
	it('同时接受时优先 br，q=0 视为不接受', () => {
		expect(chooseEncoding('gzip, deflate, br')).toBe('br');
		expect(chooseEncoding('br;q=0, gzip')).toBe('gzip');
		expect(chooseEncoding('identity')).toBeNull();
	});
});

describe('compressResponse', () => {
	it('按 Accept-Encoding 流式压缩，内容不变，并去掉 Content-Length', async () => {
		const br = compressResponse(page(html, { 'content-length': String(new TextEncoder().encode(html).byteLength) }), 'GET', 'br, gzip');
		expect(br.headers.get('content-encoding')).toBe('br');
		expect(br.headers.get('content-length')).toBeNull();
		expect(br.headers.get('vary')).toBe('accept-encoding');
		expect(new TextDecoder().decode(brotliDecompressSync(await bytes(br)))).toBe(html);

		const gz = compressResponse(page(html), 'GET', 'gzip');
		expect(gz.headers.get('content-encoding')).toBe('gzip');
		expect(new TextDecoder().decode(gunzipSync(await bytes(gz)))).toBe(html);
	});

	it('压缩后的体积明显小于原文', async () => {
		const out = await bytes(compressResponse(page(html), 'GET', 'br'));
		expect(out.byteLength).toBeLessThan(new TextEncoder().encode(html).byteLength / 5);
	});

	it('已知小于 1 KB、HEAD 请求、已编码、不可压缩类型或客户端不接受时原样返回', async () => {
		const small = compressResponse(page('<p>小</p>', { 'content-length': '12' }), 'GET', 'br');
		expect(small.headers.get('content-encoding')).toBeNull();
		expect(small.headers.get('vary')).toBe('accept-encoding');

		expect(compressResponse(page(html), 'HEAD', 'br').headers.get('content-encoding')).toBeNull();
		expect(compressResponse(page(html, { 'content-encoding': 'gzip' }), 'GET', 'br').headers.get('content-encoding')).toBe('gzip');
		const png = new Response('x'.repeat(2000), { headers: { 'content-type': 'image/png' } });
		expect(compressResponse(png, 'GET', 'br').headers.get('content-encoding')).toBeNull();

		const identity = compressResponse(page(html), 'GET', 'identity');
		expect(identity.headers.get('content-encoding')).toBeNull();
		expect(new TextDecoder().decode(await bytes(identity))).toBe(html);
	});

	it('保留状态码与其他响应头', () => {
		const res = compressResponse(
			new Response(html, { status: 404, headers: { 'content-type': 'text/html', 'cache-control': 'private, no-store' } }),
			'GET',
			'br'
		);
		expect(res.status).toBe(404);
		expect(res.headers.get('cache-control')).toBe('private, no-store');
	});
});
