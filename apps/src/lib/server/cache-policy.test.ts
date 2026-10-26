import { describe, expect, it } from 'vitest';
import { NO_STORE, pageCacheControl } from './cache-policy.ts';

describe('pageCacheControl', () => {
	it('首页与列表页可被 CDN 缓存 60 秒，过期后 300 秒内先返回旧内容', () => {
		expect(pageCacheControl({ routeId: '/', status: 200, setsCookie: false })).toBe(
			'public, max-age=0, s-maxage=60, stale-while-revalidate=300'
		);
		expect(pageCacheControl({ routeId: '/c/[slug]', status: 200, setsCookie: false })).toContain('s-maxage=60');
	});

	it('详情页缓存 120 秒', () => {
		expect(pageCacheControl({ routeId: '/d/[id]', status: 200, setsCookie: false })).toBe(
			'public, max-age=0, s-maxage=120, stale-while-revalidate=300'
		);
	});

	it('播放页、刷剧、搜索的 SSR 外壳是公共数据，可以缓存', () => {
		expect(pageCacheControl({ routeId: '/play/[id]/[n]', status: 200, setsCookie: false })).toContain('s-maxage=120');
		expect(pageCacheControl({ routeId: '/shua', status: 200, setsCookie: false })).toContain('s-maxage=60');
		expect(pageCacheControl({ routeId: '/search', status: 200, setsCookie: false })).toContain('s-maxage=60');
	});

	it('登录、注册、退出与会员中心不缓存（表单动作会写会话 Cookie）', () => {
		for (const routeId of ['/login', '/register', '/logout', '/vip', '/me/history', '/me/settings']) {
			expect(pageCacheControl({ routeId, status: 200, setsCookie: false }), routeId).toBe(NO_STORE);
		}
	});

	it('未登记的路由（含个人页）一律不缓存', () => {
		expect(pageCacheControl({ routeId: '/me', status: 200, setsCookie: false })).toBe(NO_STORE);
		expect(pageCacheControl({ routeId: '/something-new', status: 200, setsCookie: false })).toBe(NO_STORE);
		expect(pageCacheControl({ routeId: null, status: 200, setsCookie: false })).toBe(NO_STORE);
		// 演员页（S1-3a）与资讯页（S3-7）随模块删除，不再登记
		expect(pageCacheControl({ routeId: '/actors/[slug]', status: 200, setsCookie: false })).toBe(NO_STORE);
		expect(pageCacheControl({ routeId: '/news', status: 200, setsCookie: false })).toBe(NO_STORE);
		expect(pageCacheControl({ routeId: '/news/[slug]', status: 200, setsCookie: false })).toBe(NO_STORE);
	});

	it('写 Cookie 的响应与错误页不缓存，避免个人数据或临时错误进入共享缓存', () => {
		expect(pageCacheControl({ routeId: '/', status: 200, setsCookie: true })).toBe(NO_STORE);
		expect(pageCacheControl({ routeId: '/', status: 404, setsCookie: false })).toBe(NO_STORE);
		expect(pageCacheControl({ routeId: '/', status: 500, setsCookie: false })).toBe(NO_STORE);
	});
});
