import {
	CDN_ASSET_PREFIX,
	EDGE_SHARED_SECRET,
	IMAGE_BASE_URLS,
	MEDIA_CONNECT_ORIGINS,
	RATE_LIMIT_BOT_PER_MIN,
	RATE_LIMIT_PAGE_PER_MIN,
	RATE_LIMIT_READ_PER_MIN,
	RATE_LIMIT_WRITE_PER_MIN
} from '$app/env/private';
import { dev } from '$app/env';
import { clientIp, isAutomationAgent, safeEqual } from '#lib/http/request.ts';
import { splitLocale, textDirection } from '#lib/i18n/locales.ts';
import { pageCacheControl } from '#lib/server/cache-policy.ts';
import { compressResponse } from '#lib/server/compress.ts';
import { callContext } from '#lib/server/context.ts';
import { CATALOGS } from '#lib/server/i18n-catalogs.ts';
import { takeToken } from '#lib/server/rate-limit.ts';
import { getSiteConfig } from '#lib/server/site-config.ts';
import { normalizeLayout, normalizeTheme } from '#lib/theme/settings.ts';
import { fallbackRootAttributes, rootAttributes } from '#lib/theme/tokens.ts';
import type { Handle } from '@sveltejs/kit/hooks';
import type { RequestEvent } from '@sveltejs/kit';

import { documentSecurityHeaders } from '../config/security-headers';

const WINDOW_MS = 60_000;
const STATIC_FILE = /\.(?:svg|png|jpe?g|gif|webp|ico)$/;

function readCountry(request: Request): string | null {
	if (EDGE_SHARED_SECRET) {
		const provided = request.headers.get('x-edge-secret') ?? '';

		if (!safeEqual(provided, EDGE_SHARED_SECRET)) {
			return null;
		}
	}

	return (
		request.headers.get('cf-ipcountry') ??
		request.headers.get('x-vercel-ip-country') ??
		request.headers.get('cloudfront-viewer-country')
	);
}

function plain(body: string, status: number, extra?: HeadersInit): Response {
	return new Response(body, {
		status,
		headers: {
			'cache-control': 'no-store',
			'content-type': 'text/plain; charset=utf-8',
			...extra
		}
	});
}

/**
 * `<html>` 上要注入的主题属性（docs/04 C.8、W0-6）：后台配的两套配色、圆角、封面比例、
 * 默认深浅色。站点配置是全站共享的（进程内缓存 30 秒），注入内容与用户无关，页面仍可进边缘缓存。
 * 读配置失败时只给默认深浅色，颜色落到 app.css 的内置色板。
 */
async function themeAttributes(event: RequestEvent): Promise<string> {
	try {
		const config = await getSiteConfig(callContext(event));
		return rootAttributes(normalizeTheme(config.theme), normalizeLayout(config.layout));
	} catch (error) {
		console.warn('注入主题变量失败，前台用内置色板', error);
		return fallbackRootAttributes();
	}
}

function applyDocumentHeaders(response: Response, pathname: string): Response {
	for (const header of documentSecurityHeaders(!dev, CDN_ASSET_PREFIX, IMAGE_BASE_URLS, MEDIA_CONNECT_ORIGINS)) {
		response.headers.set(header.key, header.value);
	}

	if (pathname === '/service-worker.js') {
		response.headers.set('cache-control', 'no-cache, no-store, must-revalidate');
	}

	return response;
}

export const handle: Handle = async ({ event, resolve }) => {
	const { pathname } = event.url;
	const method = event.request.method.toUpperCase();
	const userAgent = event.request.headers.get('user-agent');
	const automated = isAutomationAgent(userAgent);
	const isHealth = pathname === '/api/v1/health';
	const isApi = pathname.startsWith('/api/');
	const isWrite = method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS';
	const { locale } = splitLocale(pathname);
	const messages = CATALOGS[locale];
	const isStatic =
		pathname.startsWith('/_app/') ||
		pathname === '/service-worker.js' ||
		pathname.startsWith('/icons/') ||
		STATIC_FILE.test(pathname);

	if (isApi && isWrite && !userAgent) {
		return applyDocumentHeaders(plain(messages['http.missingClient'], 403), pathname);
	}

	if (!isStatic) {
		const limit = isHealth
			? 600
			: automated
				? RATE_LIMIT_BOT_PER_MIN
				: isWrite
					? RATE_LIMIT_WRITE_PER_MIN
					: isApi
						? RATE_LIMIT_READ_PER_MIN
						: RATE_LIMIT_PAGE_PER_MIN;
		const bucket = `${automated ? 'bot' : 'user'}:${isHealth ? 'health' : isWrite ? 'write' : isApi ? 'read' : 'page'}:${clientIp(event.request.headers)}`;
		const decision = takeToken(bucket, limit, WINDOW_MS);

		if (!decision.ok) {
			return applyDocumentHeaders(
				plain(messages['http.tooManyRequests'], 429, { 'retry-after': String(decision.retryAfterSeconds) }),
				pathname
			);
		}
	}

	const requestId = crypto.randomUUID();

	event.locals.requestId = requestId;
	event.locals.country = readCountry(event.request) ?? 'LOCAL';
	event.locals.locale = locale;

	// 语言由 URL 前缀决定（docs/04 A.4），页面缓存键天然按语言区分，不需要 Vary
	const dir = textDirection(locale);
	// transformPageChunk 是同步的，主题属性要在 resolve 之前取好
	const theme = isApi || isStatic ? '' : await themeAttributes(event);
	const response = await resolve(event, {
		transformPageChunk: ({ html }) =>
			html.replace('%lang%', locale).replace('%dir%', dir).replace('%xsite.root%', theme)
	});
	const isDocument = response.headers.get('content-type')?.startsWith('text/html') ?? false;
	if (isDocument) {
		response.headers.set('content-language', locale);
	}

	// SSR 页面按路由决定能否被 CDN 缓存；接口与 load 里用 setHeaders 自定的不覆盖。
	// 客户端导航的 __data.json 由 SvelteKit 固定为 private, no-store（与部署版本绑定），这里不改。
	if (isDocument && !response.headers.has('cache-control')) {
		response.headers.set(
			'cache-control',
			pageCacheControl({
				routeId: event.route.id,
				status: response.status,
				setsCookie: response.headers.has('set-cookie')
			})
		);
	}

	response.headers.set('x-request-id', requestId);

	return compressResponse(
		applyDocumentHeaders(response, pathname),
		event.request.method,
		event.request.headers.get('accept-encoding') ?? ''
	);
};
