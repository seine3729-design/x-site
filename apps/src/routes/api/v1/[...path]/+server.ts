import { serviceHeaders, serviceUrl } from '#lib/server/api.ts';
import { callContext } from '#lib/server/context.ts';
import { clearSession, sessionAccessToken } from '#lib/server/session.ts';
import type { RequestHandler } from './$types';

// BFF 透传：浏览器只访问同源 /api/v1/*，这里转发到 Go 后端（docs/04 A.3）。只透传前台接口，
// /api/admin/* 等其他 /api/* 由 routes/api/[...path] 直接返回 404，后台接口不能经前台域名访问（docs/07 第 0.5 节）。
// 本 BFF 自己实现的接口（如 /api/v1/health）有更具体的路由，优先于这里匹配。
// 报文加密在浏览器与 Go 之间端到端完成（docs/10 第 5.3 节）：这里只原样转发密文与 X-Enc 头，
// 不解密、不改写请求目标（路径与查询串参与 AAD 校验），并标记 x-bff-proxy 让后端按浏览器请求强制加密。
// 身份：从 HttpOnly Cookie 取 Access Token 加到 Authorization 头（#lib/server/session.ts），浏览器脚本拿不到令牌。

/** 允许从浏览器转发到后端的请求头。不转发 host、x-forwarded-*、x-service-token、authorization 等，防止伪造。 */
const FORWARD_REQUEST_HEADERS = [
	'accept-language',
	'content-type',
	'if-none-match',
	'idempotency-key',
	'x-csrf-token',
	'x-device-id',
	'x-enc',
	'x-enc-type'
];

/** 允许回传给浏览器的响应头。后端不写 Cookie，会话 Cookie 只由 BFF 写。 */
const FORWARD_RESPONSE_HEADERS = [
	'cache-control',
	'content-type',
	'date',
	'etag',
	'ratelimit',
	'ratelimit-policy',
	'retry-after',
	'vary',
	'x-enc-type'
];

/**
 * 签发或吊销令牌的接口不透传：响应体是端到端加密的，BFF 读不到令牌、也就写不了 HttpOnly Cookie，
 * 透传还会让令牌落到页面脚本里。登录、注册、登出走页面的表单动作（routes/login、register、logout）。
 */
const BLOCKED = new Set([
	'/api/v1/auth/guest',
	'/api/v1/auth/login',
	'/api/v1/auth/register',
	'/api/v1/auth/refresh',
	'/api/v1/auth/logout'
]);

/** 写请求按需签发游客身份（评论等允许游客的写操作需要身份）；读请求不签发。 */
const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS']);

const TIMEOUT_MS = 10_000;

function json(status: number, code: string, message: string, requestId: string) {
	return Response.json({ code, message, request_id: requestId }, { status, headers: { 'cache-control': 'no-store' } });
}

const proxy: RequestHandler = async (event) => {
	const { request, url, cookies } = event;
	const ctx = callContext(event);
	const method = request.method.toUpperCase();

	if (BLOCKED.has(url.pathname)) {
		return json(404, 'NOT_FOUND', '接口不存在', ctx.requestId);
	}
	// 跨站写请求：会话 Cookie 是 SameSite=Lax，本来就带不过来；这里再按 Origin 拒一次（纵深防御）
	const origin = request.headers.get('origin');
	if (!SAFE_METHODS.has(method) && origin && origin !== url.origin) {
		return json(403, 'FORBIDDEN', '来源不被允许', ctx.requestId);
	}

	const incoming = new Headers();
	for (const name of FORWARD_REQUEST_HEADERS) {
		const value = request.headers.get(name);
		if (value !== null) {
			incoming.set(name, value);
		}
	}

	const headers = serviceHeaders(ctx, incoming);
	headers.set('x-bff-proxy', '1');
	const access = await sessionAccessToken(ctx, cookies, { allowGuest: !SAFE_METHODS.has(method) });
	if (access) {
		headers.set('authorization', `Bearer ${access}`);
	}

	let upstream: Response;
	try {
		upstream = await fetch(serviceUrl(url.pathname + url.search), {
			method,
			headers,
			body: SAFE_METHODS.has(method) ? undefined : await request.arrayBuffer(),
			signal: AbortSignal.timeout(TIMEOUT_MS),
			redirect: 'manual'
		});
	} catch {
		return json(503, 'UNAVAILABLE', '服务暂不可用', ctx.requestId);
	}

	// 改密成功后后端吊销了全部会话（含当前），本机 Cookie 一起清掉
	if (url.pathname === '/api/v1/me/password' && upstream.ok) {
		clearSession(cookies);
	}

	const outgoing = new Headers();
	for (const name of FORWARD_RESPONSE_HEADERS) {
		const value = upstream.headers.get(name);
		if (value !== null) {
			outgoing.set(name, value);
		}
	}

	return new Response(upstream.body, { status: upstream.status, headers: outgoing });
};

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const PATCH = proxy;
export const DELETE = proxy;
