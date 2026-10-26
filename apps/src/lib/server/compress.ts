import { pipeline, Readable } from 'node:stream';
import type { ReadableStream as NodeReadableStream } from 'node:stream/web';
import { constants, createBrotliCompress, createGzip } from 'node:zlib';

/** 已知长度小于这个值的响应不压缩，省下的字节抵不上压缩头的开销。 */
const MIN_BYTES = 1024;

const COMPRESSIBLE = new Set([
	'text/html',
	'text/plain',
	'text/css',
	'text/javascript',
	'text/xml',
	'application/javascript',
	'application/json',
	'application/manifest+json',
	'application/xml',
	'application/xhtml+xml',
	'image/svg+xml'
]);

export function mediaType(header: string): string {
	return header.split(';', 1)[0]?.trim().toLowerCase() ?? '';
}

export function isCompressible(contentType: string): boolean {
	return COMPRESSIBLE.has(mediaType(contentType));
}

/** 客户端同时接受 br 和 gzip 时用 Brotli。q=0 视为不接受。 */
export function chooseEncoding(header: string): 'br' | 'gzip' | null {
	const quality = new Map<string, number>();

	for (const part of header.split(',')) {
		const [rawName = '', ...params] = part.trim().toLowerCase().split(';');
		const name = rawName.trim();

		if (!name) {
			continue;
		}

		const qParam = params.map((param) => param.trim()).find((param) => param.startsWith('q='));
		const q = qParam ? Number(qParam.slice(2)) : 1;

		if (Number.isFinite(q)) {
			quality.set(name, q);
		}
	}

	const br = quality.has('br') ? quality.get('br')! : (quality.get('*') ?? 0);
	const gzip = quality.has('gzip') ? quality.get('gzip')! : (quality.get('*') ?? 0);

	if (br > 0) {
		return 'br';
	}

	if (gzip > 0) {
		return 'gzip';
	}

	return null;
}

export function appendVary(headers: Headers, token: string) {
	const current = headers.get('vary');

	if (!current) {
		headers.set('vary', token);
		return;
	}

	const listed = current.split(',').map((part) => part.trim().toLowerCase());

	if (listed.includes('*') || listed.includes(token)) {
		return;
	}

	headers.set('vary', `${current}, ${token}`);
}

/**
 * 源站动态压缩（未经 CDN 的流量才需要，docs/08 第 5 节）。
 *
 * 用 node:zlib 的流式压缩器：压缩在线程池里进行，不阻塞事件循环；也不需要先把整个响应读进内存，
 * SSR 的流式输出照样逐块送达。Brotli 用质量 5：实测首页约 0.2 ms，默认的质量 11 约 19 ms、体积只小 12%，
 * 不适合按请求压缩。不要换成 CompressionStream('brotli')：Bun 下它就是质量 11，而且在主线程执行。
 * HEAD 请求、已编码、不可压缩的类型、客户端不接受或已知小于 1 KB 时原样返回（可压缩类型仍补上 Vary）。
 */
export function compressResponse(response: Response, method: string, acceptEncoding: string): Response {
	if (
		method === 'HEAD' ||
		response.body === null ||
		response.headers.has('content-encoding') ||
		!isCompressible(response.headers.get('content-type') ?? '')
	) {
		return response;
	}

	const headers = new Headers(response.headers);
	appendVary(headers, 'accept-encoding');

	const init = { status: response.status, statusText: response.statusText, headers };
	const encoding = chooseEncoding(acceptEncoding);
	const length = Number(headers.get('content-length'));

	if (!encoding || (headers.has('content-length') && length < MIN_BYTES)) {
		return new Response(response.body, init);
	}

	headers.delete('content-length');
	headers.set('content-encoding', encoding);

	const compressor =
		encoding === 'br'
			? createBrotliCompress({ params: { [constants.BROTLI_PARAM_QUALITY]: 5 } })
			: createGzip();
	// 源头出错或客户端断开时 pipeline 会销毁整条流，响应随之中止
	const source = Readable.fromWeb(response.body as unknown as NodeReadableStream<Uint8Array>);
	const body = Readable.toWeb(pipeline(source, compressor, () => {})) as unknown as ReadableStream<Uint8Array>;

	return new Response(body, init);
}
