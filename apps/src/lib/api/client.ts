/**
 * 浏览器调用 /api/v1/* 的统一入口（docs/04 A.5）：全部经报文加密客户端 secureApi，附带设备标识与界面语言。
 * 会话令牌在 HttpOnly Cookie 里，由 BFF 透传时加到 Authorization 头，这里不接触任何令牌。
 *
 * 加密模块约 5 KB（br），页面里按需 `await import('#lib/api/client.ts')`，不进首屏包。
 */
import { ApiError, secureApi, type SecureRequestInit } from '#lib/crypto/api-client.ts';
import { deviceId } from '#lib/local/device.ts';

export { ApiError };

let currentLocale = 'zh-CN';

/** 根 layout 在语言变化时调用，后端按 Accept-Language 处理文案类数据。 */
export function setApiLocale(locale: string) {
	currentLocale = locale;
}

function requestHeaders(extra?: HeadersInit): Headers {
	const headers = new Headers(extra);
	if (!headers.has('accept')) {
		headers.set('accept', 'application/json');
	}
	headers.set('accept-language', currentLocale);
	const id = deviceId();
	if (id) {
		headers.set('x-device-id', id);
	}
	return headers;
}

/**
 * 带设备标识的加密请求（给 hls.js 取清单与密钥用）：播放令牌绑定选源时的 X-Device-Id，
 * 清单与密钥请求必须带同一个标识，否则后端按「不是签给本设备」返回 403。
 */
export function deviceSecureFetch(path: string, init: SecureRequestInit = {}): Promise<Response> {
	const headers = new Headers(init.headers);
	const id = deviceId();
	if (id) {
		headers.set('x-device-id', id);
	}
	return secureApi.secureFetch(path, { ...init, headers });
}

type Envelope<T> = { code?: string; message?: string; data?: T; request_id?: string };

export type ApiRequestOptions = {
	signal?: AbortSignal;
	headers?: HeadersInit;
};

type LegacyOrOptions = AbortSignal | ApiRequestOptions | undefined;

function normalizeOptions(input: LegacyOrOptions): ApiRequestOptions {
	if (!input) return {};
	if (typeof AbortSignal !== 'undefined' && input instanceof AbortSignal) {
		return { signal: input };
	}
	return input;
}

async function send<T>(method: string, path: string, body: unknown, options: ApiRequestOptions = {}): Promise<T> {
	let response: Response;
	try {
		response = await secureApi.secureFetch(path, {
			method,
			headers: requestHeaders(options.headers),
			body: body === undefined ? undefined : JSON.stringify(body),
			signal: options.signal,
		});
	} catch (err) {
		if (options.signal?.aborted) {
			throw err;
		}
		throw new ApiError(0, err instanceof ApiError ? err.code : 'NETWORK', err instanceof Error ? err.message : '');
	}
	if (response.status === 204 || response.status === 304) {
		return undefined as T;
	}
	const envelope = (await response.json().catch(() => null)) as Envelope<T> | null;
	if (!response.ok || envelope?.code !== 'OK') {
		throw new ApiError(response.status, envelope?.code ?? 'INTERNAL', envelope?.message ?? '', envelope?.request_id);
	}
	return envelope.data as T;
}

export const api = {
	get: <T>(path: string, options?: LegacyOrOptions) => send<T>('GET', path, undefined, normalizeOptions(options)),
	post: <T = void>(path: string, body?: unknown, options?: LegacyOrOptions) => send<T>('POST', path, body ?? {}, normalizeOptions(options)),
	patch: <T = void>(path: string, body: unknown, options?: LegacyOrOptions) => send<T>('PATCH', path, body, normalizeOptions(options)),
	del: <T = void>(path: string, options?: LegacyOrOptions) => send<T>('DELETE', path, undefined, normalizeOptions(options)),
};

/** 预留接口（docs/07 已列出、后端尚未实现）返回 404 时按空处理，其余错误照常抛出。 */
export async function reserved<T>(promise: Promise<T>, fallback: T): Promise<T> {
	try {
		return await promise;
	} catch (err) {
		if (err instanceof ApiError && err.status === 404) {
			return fallback;
		}
		throw err;
	}
}

export function isAuthError(err: unknown): boolean {
	return err instanceof ApiError && (err.status === 401 || err.code === 'NEED_LOGIN' || err.code === 'TOKEN_EXPIRED');
}
