import type { Reroute } from '@sveltejs/kit/hooks';
import { splitLocale } from '#lib/i18n/locales.ts';

/**
 * 语言前缀不对应真实路由：/en/d/1 按 /d/1 匹配，地址栏与 event.url 保持原样（docs/04 A.4）。
 * /api/* 不带语言前缀，/en/api/... 不重写，直接 404。
 */
export const reroute: Reroute = ({ url }) => {
	const { prefixed, path } = splitLocale(url.pathname);
	if (!prefixed || path === '/api' || path.startsWith('/api/')) {
		return;
	}
	return path;
};
