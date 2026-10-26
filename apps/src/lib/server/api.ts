import { BFF_SERVICE_TOKEN, SERVICE_INTERNAL_URL } from '$app/env/private';

/** 后端统一响应包（docs/07 第 0.1 节）。 */
export type Envelope<T> = {
	code: string;
	message: string;
	data?: T;
	request_id?: string;
};

/** 后端返回的业务错误。BFF 据此决定页面状态（登录墙、404、限流）。 */
export class ServiceError extends Error {
	constructor(
		readonly status: number,
		readonly code: string,
		message: string,
		readonly requestId?: string,
		options?: ErrorOptions
	) {
		super(message, options);
		this.name = 'ServiceError';
	}
}

/** 调用后端时需要的请求上下文（来自 SvelteKit 的 RequestEvent）。 */
export type CallContext = {
	requestId: string;
	clientIp: string;
	userAgent: string | null;
};

const DEFAULT_TIMEOUT_MS = 5_000;

/** 组装发往后端的请求头：服务令牌、request id、真实客户端 IP。 */
export function serviceHeaders(ctx: CallContext, extra?: HeadersInit): Headers {
	const headers = new Headers(extra);
	headers.set('accept', 'application/json');
	headers.set('x-request-id', ctx.requestId);
	headers.set('x-forwarded-for', ctx.clientIp);
	if (ctx.userAgent) {
		headers.set('user-agent', ctx.userAgent);
	}
	if (BFF_SERVICE_TOKEN) {
		headers.set('x-service-token', BFF_SERVICE_TOKEN);
	}
	return headers;
}

/** 后端地址拼接。path 必须以 / 开头。 */
export function serviceUrl(path: string): string {
	return `${SERVICE_INTERNAL_URL}${path}`;
}

/**
 * 以 JSON 方式调用后端并解包 data。
 * GET 在网络错误时重试一次；超时默认 5 秒；非 2xx 抛出 ServiceError。
 */
export async function callService<T>(
	ctx: CallContext,
	path: string,
	init: RequestInit & { timeoutMs?: number } = {}
): Promise<{ data: T; response: Response }> {
	const method = (init.method ?? 'GET').toUpperCase();
	const attempts = method === 'GET' ? 2 : 1;
	let lastError: unknown;

	for (let attempt = 1; attempt <= attempts; attempt += 1) {
		try {
			const response = await fetch(serviceUrl(path), {
				...init,
				method,
				headers: serviceHeaders(ctx, init.headers),
				signal: AbortSignal.timeout(init.timeoutMs ?? DEFAULT_TIMEOUT_MS)
			});

			if (response.status === 304) {
				return { data: undefined as T, response };
			}

			const body = (await response.json().catch(() => null)) as Envelope<T> | null;

			if (!response.ok || !body || body.code !== 'OK') {
				throw new ServiceError(
					response.status,
					body?.code ?? 'INTERNAL',
					body?.message ?? '服务暂不可用',
					body?.request_id ?? response.headers.get('x-request-id') ?? undefined
				);
			}

			return { data: body.data as T, response };
		} catch (error) {
			// 业务错误不重试；只有网络错误与超时才重试
			if (error instanceof ServiceError) {
				throw error;
			}
			lastError = error;
		}
	}

	throw new ServiceError(503, 'UNAVAILABLE', '服务暂不可用', ctx.requestId, { cause: lastError });
}
