/**
 * SSR 页面 HTML 的缓存策略（docs/04 E 节、docs/08 第 3 节）。客户端导航的 __data.json 由 SvelteKit 固定为 no-store。
 *
 * 只有登记在这里的公共路由会被 CDN 缓存；个人页、未登记的路由、错误页、写 Cookie 的响应一律不缓存——
 * 宁可少缓存，也不能让个人数据进入共享缓存。缓存键不含登录态，所以登记的页面其 load 不能返回个人数据，
 * 也不能按 Cookie 输出不同内容（个性化部分由客户端二次请求填充）。新增公共页面时在这里登记。
 */

export const NO_STORE = 'private, no-store';

// 浏览器每次都回源验证（max-age=0），CDN 缓存 s-maxage 秒，过期后 300 秒内先返回旧内容并在后台刷新
const LIST = 'public, max-age=0, s-maxage=60, stale-while-revalidate=300';
const DETAIL = 'public, max-age=0, s-maxage=120, stale-while-revalidate=300';

/** 路由 ID（与 src/routes 的目录结构一致）→ Cache-Control。 */
const PUBLIC_ROUTES: Record<string, string> = {
	'/': LIST,
	'/explore': LIST,
	'/c/[slug]': LIST,
	'/tag/[slug]': LIST,
	'/topics': LIST,
	'/rank': LIST,
	'/search': LIST,
	'/shua': LIST,
	'/d/[id]': DETAIL,
	// 播放页 SSR 只有剧与分集的公共数据；选源、观看权限、续播位置都由浏览器二次请求
	'/play/[id]/[n]': DETAIL,
	'/topics/[slug]': DETAIL,
	'/p/[slug]': DETAIL,
	'/way-home': DETAIL
};

export function pageCacheControl({
	routeId,
	status,
	setsCookie
}: {
	routeId: string | null;
	status: number;
	setsCookie: boolean;
}): string {
	if (status !== 200 || setsCookie || !routeId) {
		return NO_STORE;
	}

	return PUBLIC_ROUTES[routeId] ?? NO_STORE;
}
