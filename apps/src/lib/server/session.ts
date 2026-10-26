/**
 * Web 会话（docs/04 A.3、docs/10 第 6.1 节）：令牌只放 HttpOnly Cookie，浏览器脚本读不到。
 *
 * - 登录、注册、登出由 BFF 的表单动作经内网调用后端完成，BFF 拿到令牌后写 Cookie；
 *   浏览器到 Go 的报文是端到端加密的，BFF 读不到透传响应里的令牌，所以签发令牌的接口不走透传。
 * - 浏览器经 /api/v1/* 透传时，BFF 从 Cookie 取 Access Token 加到 Authorization 头；快过期时先用
 *   Refresh Token 轮换（同一个 Refresh Token 只轮换一次，见 refreshTokens）。
 * - SSR 页面不读会话：公共页面要进边缘缓存，个人数据由浏览器二次请求。
 * - 另写一个脚本可读的 xs_li=1（不含任何身份信息），只用来让页面判断要不要请求 /me/state。
 */
import { dev } from '$app/env';
import type { Cookies } from '@sveltejs/kit';
import { callService, ServiceError, type CallContext } from '#lib/server/api.ts';

const SECURE_PREFIX = dev ? '' : '__Host-';

export const SESSION_COOKIES = {
	access: `${SECURE_PREFIX}xs_at`,
	refresh: `${SECURE_PREFIX}xs_rt`,
	/** 登录提示（值固定为 1），脚本可读 */
	hint: 'xs_li'
} as const;

export type Role = 'guest' | 'user';

export type SessionTokens = {
	access_token: string;
	refresh_token: string;
	expires_in: number;
	role: Role;
};

const DAY = 86_400;
/** 与后端一致：注册用户 Refresh 30 天滑动，游客 180 天（docs/10 第 4 节） */
const REFRESH_MAX_AGE: Record<Role, number> = { user: 30 * DAY, guest: 180 * DAY };
/** Access Token 剩余不足这么多秒就先轮换，避免请求在路上过期 */
const REFRESH_EARLY_SEC = 60;

function cookieOptions(maxAge: number, httpOnly = true) {
	return { path: '/', httpOnly, sameSite: 'lax' as const, secure: !dev, maxAge };
}

export function writeSession(cookies: Cookies, tokens: SessionTokens) {
	cookies.set(SESSION_COOKIES.access, tokens.access_token, cookieOptions(Math.max(60, tokens.expires_in)));
	cookies.set(SESSION_COOKIES.refresh, tokens.refresh_token, cookieOptions(REFRESH_MAX_AGE[tokens.role]));
	if (tokens.role === 'user') {
		cookies.set(SESSION_COOKIES.hint, '1', cookieOptions(REFRESH_MAX_AGE.user, false));
	} else {
		cookies.delete(SESSION_COOKIES.hint, { path: '/', secure: !dev });
	}
}

export function clearSession(cookies: Cookies) {
	for (const name of Object.values(SESSION_COOKIES)) {
		cookies.delete(name, { path: '/', secure: !dev });
	}
}

/**
 * 读出 JWT 载荷里的 exp 与 role（不验签：只用来决定何时轮换与界面提示，鉴权仍由后端验签）。
 * 格式不对时返回 null，按「没有令牌」处理。
 */
export function peekAccessToken(token: string): { exp: number; role: Role } | null {
	const payload = token.split('.')[1];
	if (!payload) {
		return null;
	}
	try {
		const json = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8')) as { exp?: unknown; role?: unknown };
		if (typeof json.exp !== 'number' || (json.role !== 'user' && json.role !== 'guest')) {
			return null;
		}
		return { exp: json.exp, role: json.role };
	} catch {
		return null;
	}
}

/** 正在轮换的 Refresh Token → 结果；轮换完成后在 RECENT_MS 内复用同一结果。 */
const refreshing = new Map<string, Promise<SessionTokens | null>>();
const recent = new Map<string, { tokens: SessionTokens; at: number }>();
const RECENT_MS = 30_000;

function pruneRecent(now: number) {
	for (const [key, value] of recent) {
		if (now - value.at > RECENT_MS) {
			recent.delete(key);
		}
	}
}

/**
 * 用 Refresh Token 换新令牌。后端每次轮换都会作废旧令牌，旧令牌再用一次会被判定泄露并吊销整个会话族，
 * 所以同一页面并发的几个请求必须共用一次轮换：进行中的合并，完成后 30 秒内拿旧令牌来的请求直接复用结果
 * （浏览器可能还没收到新 Cookie）。多个 BFF 实例之间不共享这份记录，部署时同一用户的请求应尽量落在同一实例
 * （或在后端给轮换加宽限期，见 docs/04 A.3）。返回 null 表示令牌已失效，需要重新登录。
 */
export async function refreshTokens(ctx: CallContext, refreshToken: string): Promise<SessionTokens | null> {
	const now = Date.now();
	pruneRecent(now);
	const memo = recent.get(refreshToken);
	if (memo) {
		return memo.tokens;
	}
	let pending = refreshing.get(refreshToken);
	if (!pending) {
		pending = callService<SessionTokens>(ctx, '/api/v1/auth/refresh', {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({ refresh_token: refreshToken }),
			timeoutMs: 4_000
		})
			.then(({ data }) => {
				recent.set(refreshToken, { tokens: data, at: Date.now() });
				return data;
			})
			.catch((err: unknown) => {
				if (err instanceof ServiceError && err.status === 401) {
					return null;
				}
				throw err;
			})
			.finally(() => refreshing.delete(refreshToken));
		refreshing.set(refreshToken, pending);
	}
	return pending;
}

/** 签发游客身份（只在需要身份的写请求里按需调用，不在页面请求里签发，免得首访页面写 Cookie 失去边缘缓存）。 */
export async function issueGuest(ctx: CallContext): Promise<SessionTokens | null> {
	try {
		const { data } = await callService<SessionTokens>(ctx, '/api/v1/auth/guest', {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({ platform: 'web' }),
			timeoutMs: 4_000
		});
		return data;
	} catch (err) {
		console.warn('签发游客身份失败', err instanceof ServiceError ? err.code : err);
		return null;
	}
}

/**
 * 取当前请求可用的 Access Token：Cookie 里的还有效就直接用；快过期或已过期时用 Refresh Token 轮换并写回 Cookie；
 * 都没有且 allowGuest 时签发游客身份。轮换失败（令牌被吊销）时清掉会话 Cookie。
 */
export async function sessionAccessToken(
	ctx: CallContext,
	cookies: Cookies,
	{ allowGuest }: { allowGuest: boolean }
): Promise<string | null> {
	const access = cookies.get(SESSION_COOKIES.access);
	const peek = access ? peekAccessToken(access) : null;
	if (access && peek && peek.exp - REFRESH_EARLY_SEC > Date.now() / 1000) {
		return access;
	}
	const refresh = cookies.get(SESSION_COOKIES.refresh);
	if (refresh) {
		try {
			const tokens = await refreshTokens(ctx, refresh);
			if (tokens) {
				writeSession(cookies, tokens);
				return tokens.access_token;
			}
			clearSession(cookies);
		} catch (err) {
			// 后端暂时不可用：令牌未必失效，不清 Cookie，本次按无身份转发
			console.warn('轮换令牌失败', err instanceof ServiceError ? err.code : err);
			return null;
		}
	}
	if (allowGuest) {
		const guest = await issueGuest(ctx);
		if (guest) {
			writeSession(cookies, guest);
			return guest.access_token;
		}
	}
	return null;
}

/** 站内跳转目标只收本站路径，防止登录后被带去外站（开放重定向）。 */
export function safeNext(value: string | null | undefined, fallback = '/'): string {
	if (!value || !value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\') || value.startsWith('/api/')) {
		return fallback;
	}
	return value;
}
