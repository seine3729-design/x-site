/**
 * 浏览器侧加密接口客户端（docs/10 第 5.3 节，协议与 service/internal/crypto/apienc 一致）。
 *
 * 浏览器发往 /api/v1/* 的请求都应走这里：每个请求用服务端公钥做一次 HPKE 封装，请求体加密，
 * 响应体用本次请求导出的密钥解密，调用方拿到的是普通的明文 Response。
 * SSR 阶段的 BFF 调用走内网，不经过这里（见 #lib/server/api.ts）。
 */
import { base64url, concat, fromBase64url, setupBaseSender, utf8Bytes, type Bytes } from './hpke.ts';

export const ENC_CONTENT_TYPE = 'application/vnd.xsite.enc';
const INFO = 'xsite-api/v1';
const RESPONSE_LABEL = 'xsite-api/v1 response';
const KEYS_PATH = '/api/v1/crypto/keys';

type ServerKey = { kid: string; publicKey: Bytes };

/** 只需要标准 fetch 的调用签名（Bun 的 typeof fetch 额外带 preconnect）。 */
export type FetchLike = (input: string | URL | Request, init?: RequestInit) => Promise<Response>;

export type SecureClientOptions = {
	/** 接口地址前缀。浏览器里留空（同源），脚本与测试里传完整地址。 */
	baseUrl?: string;
	fetch?: FetchLike;
	/** 当前 Unix 毫秒时间，测试可替换。 */
	now?: () => number;
	/** 只用于测试：固定 HPKE 临时私钥。 */
	fixedEphemeralKey?: Bytes;
};

export type SecureRequestInit = Omit<RequestInit, 'body'> & {
	/** 请求体明文（JSON 字符串或字节）。 */
	body?: string | Uint8Array;
};

/** 构造加密请求所需的 AAD：方法、请求目标（路径 + 查询串）与时间戳。 */
export function requestAAD(method: string, requestTarget: string, ts: number): Bytes {
	return utf8Bytes(`${INFO}\n${method.toUpperCase()}\n${requestTarget}\n${ts}`);
}

/** 本次请求的加密结果：X-Enc 头、加密后的请求体，以及用于解密响应的密钥。 */
export type SealedRequest = { header: string; body: Bytes | null; responseKey: Bytes };

export async function sealRequest(
	key: ServerKey,
	method: string,
	requestTarget: string,
	ts: number,
	plaintext: Bytes | null,
	fixedEphemeralKey?: Bytes
): Promise<SealedRequest> {
	const ctx = await setupBaseSender(key.publicKey, utf8Bytes(INFO), fixedEphemeralKey);
	const aad = requestAAD(method, requestTarget, ts);
	// tag：空明文的 Seal，让没有请求体的 GET 也能校验路径、查询串与时间戳
	const tag = await ctx.seal(aad, new Uint8Array(0));
	const body = plaintext && plaintext.byteLength > 0 ? await ctx.seal(aad, plaintext) : null;
	const responseKey = await ctx.export(utf8Bytes(RESPONSE_LABEL), 32);
	const header = ['1', key.kid, String(ts), base64url(ctx.enc), base64url(tag)].join('.');
	return { header, body, responseKey };
}

/** 解密响应体：nonce(12) || AES-256-GCM 密文。 */
export async function openResponse(responseKey: Bytes, data: Bytes): Promise<Bytes> {
	if (data.byteLength < 12 + 16) {
		throw new EncryptionError('ENC_INVALID', '响应报文过短');
	}
	const key = await crypto.subtle.importKey('raw', responseKey, 'AES-GCM', false, ['decrypt']);
	const plain = await crypto.subtle.decrypt(
		{ name: 'AES-GCM', iv: data.subarray(0, 12), additionalData: utf8Bytes(RESPONSE_LABEL) },
		key,
		data.subarray(12)
	);
	return new Uint8Array(plain);
}

export class EncryptionError extends Error {
	constructor(
		readonly code: string,
		message: string
	) {
		super(message);
		this.name = 'EncryptionError';
	}
}

/** 解密前的协议错误码（服务端以明文返回），客户端据此刷新公钥或校准时间后重试一次。 */
const RETRYABLE = new Set(['ENC_KEY_UNKNOWN', 'ENC_TIME_SKEW']);

export function createSecureClient(options: SecureClientOptions = {}) {
	const baseUrl = options.baseUrl ?? '';
	const doFetch = options.fetch ?? ((input, init) => fetch(input, init));
	const now = options.now ?? Date.now;
	let keyPromise: Promise<ServerKey> | null = null;
	let clockOffsetMs = 0;

	function absolute(path: string): URL {
		const origin = baseUrl || (typeof location === 'undefined' ? 'http://localhost' : location.origin);
		return new URL(path, origin);
	}

	async function fetchKey(): Promise<ServerKey> {
		const response = await doFetch(absolute(KEYS_PATH), { headers: { accept: 'application/json' } });
		const body = (await response.json().catch(() => null)) as {
			code?: string;
			data?: { current: string; keys: { kid: string; public_key: string }[] };
		} | null;
		const current = body?.data?.keys.find((k) => k.kid === body.data?.current);
		if (!response.ok || body?.code !== 'OK' || !current) {
			throw new EncryptionError('ENC_KEYS_UNAVAILABLE', '无法获取加密公钥');
		}
		return { kid: current.kid, publicKey: fromBase64url(current.public_key) };
	}

	function serverKey(): Promise<ServerKey> {
		keyPromise ??= fetchKey().catch((error: unknown) => {
			keyPromise = null;
			throw error;
		});
		return keyPromise;
	}

	async function attempt(url: URL, init: SecureRequestInit, plaintext: Bytes | null) {
		const method = (init.method ?? 'GET').toUpperCase();
		const ts = Math.floor((now() + clockOffsetMs) / 1000);
		const sealed = await sealRequest(
			await serverKey(),
			method,
			url.pathname + url.search,
			ts,
			plaintext,
			options.fixedEphemeralKey
		);
		const headers = new Headers(init.headers);
		headers.set('x-enc', sealed.header);
		if (sealed.body) {
			headers.set('content-type', ENC_CONTENT_TYPE);
			headers.set('x-enc-type', 'application/json');
		}
		const response = await doFetch(url, { ...init, method, headers, body: sealed.body ?? undefined });
		return { response, responseKey: sealed.responseKey };
	}

	/** 发送加密请求，返回解密后的 Response（content-type 恢复为服务端原始类型）。 */
	async function secureFetch(path: string, init: SecureRequestInit = {}): Promise<Response> {
		const url = absolute(path);
		const plaintext =
			init.body === undefined ? null : typeof init.body === 'string' ? utf8Bytes(init.body) : concat(init.body);

		let { response, responseKey } = await attempt(url, init, plaintext);
		const retryCode = await protocolErrorCode(response);
		if (retryCode && RETRYABLE.has(retryCode)) {
			if (retryCode === 'ENC_KEY_UNKNOWN') {
				keyPromise = null;
			} else {
				const serverTime = Date.parse(response.headers.get('date') ?? '');
				if (Number.isFinite(serverTime)) {
					clockOffsetMs = serverTime - now();
				}
			}
			({ response, responseKey } = await attempt(url, init, plaintext));
		}
		return decrypt(response, responseKey);
	}

	/** 便捷方法：发送 JSON，解包 `{ code, message, data }`，非 OK 时抛出 ApiError。 */
	async function json<T>(path: string, init: Omit<SecureRequestInit, 'body'> & { json?: unknown } = {}): Promise<T> {
		const { json: payload, ...rest } = init;
		const response = await secureFetch(path, {
			...rest,
			body: payload === undefined ? undefined : JSON.stringify(payload)
		});
		const body = (await response.json().catch(() => null)) as {
			code?: string;
			message?: string;
			data?: T;
			request_id?: string;
		} | null;
		if (!response.ok || body?.code !== 'OK') {
			throw new ApiError(response.status, body?.code ?? 'INTERNAL', body?.message ?? '', body?.request_id);
		}
		return body.data as T;
	}

	return { secureFetch, json };
}

export class ApiError extends Error {
	constructor(
		readonly status: number,
		readonly code: string,
		message: string,
		readonly requestId?: string
	) {
		super(message);
		this.name = 'ApiError';
	}
}

async function protocolErrorCode(response: Response): Promise<string | null> {
	if (response.status !== 400 || !response.headers.get('content-type')?.startsWith('application/json')) {
		return null;
	}
	const body = (await response.clone().json().catch(() => null)) as { code?: string } | null;
	return body?.code?.startsWith('ENC_') ? body.code : null;
}

async function decrypt(response: Response, responseKey: Bytes): Promise<Response> {
	if (response.headers.get('content-type') !== ENC_CONTENT_TYPE) {
		return response; // 304/204、解密前的协议错误、豁免接口
	}
	const plain = await openResponse(responseKey, new Uint8Array(await response.arrayBuffer()));
	const headers = new Headers(response.headers);
	headers.set('content-type', response.headers.get('x-enc-type') ?? 'application/octet-stream');
	headers.delete('content-length');
	headers.delete('x-enc-type');
	return new Response(plain, { status: response.status, statusText: response.statusText, headers });
}

/** 浏览器里共用的同源客户端。 */
export const secureApi = createSecureClient();
