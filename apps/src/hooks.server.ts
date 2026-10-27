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
import { mergeDomains } from '#lib/media/domains.ts';
import { takeToken } from '#lib/server/rate-limit.ts';
import { getSiteConfig, type SiteConfig } from '#lib/server/site-config.ts';
import { normalizeLayout, normalizeTheme } from '#lib/theme/settings.ts';
import { fallbackRootAttributes, rootAttributes } from '#lib/theme/tokens.ts';
import type { Handle } from '@sveltejs/kit/hooks';
import type { RequestEvent } from '@sveltejs/kit';

import { documentSecurityHeaders } from '../config/security-headers';

const WINDOW_MS = 60_000;
const STATIC_FILE = /\.(?:svg|png|jpe?g|gif|webp|ico)$/;

/**
 * 产物走 CDN 时补一条**不带 crossorigin** 的预连接（填进 app.html 的 %xsite.preconnect%）。
 *
 * 只补这一条：`paths.assets` 非空时 SvelteKit 自己已经往 `<head>` 写了两条 preconnect，但两条都带
 * `crossorigin`。浏览器的连接池按「是否带 CORS」分开 —— 模块脚本与样式表一律按 CORS 模式取（那两条
 * 已经覆盖），而 `<link rel=icon>` 与直接用 `<img>` 取 CDN 上的图片是 no-cors，少了这一条还要再
 * 握一次手。admin 与 feedback 的入口 HTML 同样各写两条，原因一样。
 */
const CDN_PRECONNECT = ((): string => {
	if (!CDN_ASSET_PREFIX) {
		return '';
	}

	try {
		return `<link rel="preconnect" href="${new URL(CDN_ASSET_PREFIX).origin}" />`;
	} catch {
		// env.ts 已经校验过格式，这里只是兜底，不值得让页面挂掉
		return '';
	}
})();

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

/** CSP 要列出的两个域名池（docs/04 F）。 */
type DomainPools = { image: string[]; media: string[] };

/**
 * 文档请求要用站点配置的两处：注入主题变量（C.8）与生成 CSP 的域名池（F）。
 * 两者读同一份配置，所以只取一次（`getSiteConfig` 本身有 30 秒进程内缓存与单飞）。
 * 后端不可用时返回 null，由下面两个函数各自兜底。
 */
async function siteConfigOrNull(event: RequestEvent): Promise<SiteConfig | null> {
	try {
		return await getSiteConfig(callContext(event));
	} catch (error) {
		console.warn('读取站点配置失败：主题用内置色板，CSP 域名池退回部署配置', error);
		return null;
	}
}

/**
 * `<html>` 上要注入的主题属性（docs/04 C.8、W0-6）：后台配的两套配色、圆角、封面比例、
 * 默认深浅色。注入内容与用户无关，页面仍可进边缘缓存。
 * 读配置失败时只给默认深浅色，颜色落到 app.css 的内置色板。
 */
function themeAttributes(config: SiteConfig | null): string {
	if (!config) {
		return fallbackRootAttributes();
	}
	return rootAttributes(normalizeTheme(config.theme), normalizeLayout(config.layout));
}

/**
 * CSP 的域名池按请求生成（docs/04 F）：主来源是随站点配置下发的后台线路表，
 * 部署配置是补充（后台手填的完整封面 / Logo 地址不在线路表里）与后端不可用时的兜底。
 * 这样后台换一条图片 / 视频线路，前台的 connect-src 跟着变，不需要改部署变量再重启。
 */
function domainPools(config: SiteConfig | null): DomainPools {
	return {
		image: mergeDomains(config?.media?.image_bases, IMAGE_BASE_URLS),
		media: mergeDomains(config?.media?.video_origins, MEDIA_CONNECT_ORIGINS)
	};
}

function applyDocumentHeaders(response: Response, pathname: string, pools: DomainPools): Response {
	for (const header of documentSecurityHeaders(!dev, CDN_ASSET_PREFIX, pools.image, pools.media)) {
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

	// 被挡下的请求（403 / 429）返回的是纯文本，不读站点配置：既没必要，也不该让被限流的请求回源打后端。
	const envPools = domainPools(null);

	if (isApi && isWrite && !userAgent) {
		return applyDocumentHeaders(plain(messages['http.missingClient'], 403), pathname, envPools);
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
				pathname,
				envPools
			);
		}
	}

	const requestId = crypto.randomUUID();

	event.locals.requestId = requestId;
	event.locals.country = readCountry(event.request) ?? 'LOCAL';
	event.locals.locale = locale;

	// 语言由 URL 前缀决定（docs/04 A.4），页面缓存键天然按语言区分，不需要 Vary
	const dir = textDirection(locale);
	// transformPageChunk 是同步的，主题属性要在 resolve 之前取好；CSP 的域名池读的是同一份配置
	const needsConfig = !isApi && !isStatic;
	const config = needsConfig ? await siteConfigOrNull(event) : null;
	const theme = needsConfig ? themeAttributes(config) : '';
	const pools = needsConfig ? domainPools(config) : envPools;
	const response = await resolve(event, {
		transformPageChunk: ({ html }) =>
			html
				.replace('%lang%', locale)
				.replace('%dir%', dir)
				.replace('%xsite.root%', theme)
				.replace('%xsite.preconnect%', CDN_PRECONNECT)
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
		applyDocumentHeaders(response, pathname, pools),
		event.request.method,
		event.request.headers.get('accept-encoding') ?? ''
	);
};
