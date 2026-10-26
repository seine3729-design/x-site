import { describe, expect, it } from 'vitest';
import { announcementPage, DEFAULT_TABBAR, DEFAULT_TOP, immersiveRoute, isActive } from './nav.ts';

describe('isActive', () => {
	it('首页只在首页高亮', () => {
		expect(isActive('/', '/')).toBe(true);
		expect(isActive('/', '/explore')).toBe(false);
	});

	it('其余按路径段前缀，不会把 /rank 当成 /ranking 的父级', () => {
		expect(isActive('/explore', '/explore')).toBe(true);
		expect(isActive('/topics', '/topics/uiseed-topic')).toBe(true);
		expect(isActive('/rank', '/ranking')).toBe(false);
		expect(isActive('/me', '/me/history')).toBe(true);
	});
});

describe('immersiveRoute', () => {
	it('播放页手机上不显示顶栏、底栏、页脚', () => {
		expect(immersiveRoute('/play/[id]/[n]')).toEqual({ header: false, tabbar: false, footer: false });
	});

	it('刷剧页留着底栏方便切换', () => {
		expect(immersiveRoute('/shua')).toEqual({ header: false, tabbar: true, footer: false });
	});

	it('其余页面三者都在', () => {
		for (const routeId of ['/', '/d/[id]', '/me', null]) {
			expect(immersiveRoute(routeId)).toEqual({ header: true, tabbar: true, footer: true });
		}
	});
});

describe('announcementPage', () => {
	it('路由 id 映射到公告的「出现页面」', () => {
		expect(announcementPage('/')).toBe('home');
		expect(announcementPage('/c/[slug]')).toBe('channel');
		expect(announcementPage('/topics')).toBe('topic');
		expect(announcementPage('/topics/[slug]')).toBe('topic');
		expect(announcementPage('/play/[id]/[n]')).toBe('play');
	});

	it('个人中心下的所有页面都算 me', () => {
		expect(announcementPage('/me')).toBe('me');
		expect(announcementPage('/me/history')).toBe('me');
		expect(announcementPage('/me/settings')).toBe('me');
	});

	it('没有对应页面键时返回 null（如登录页、静态页）', () => {
		expect(announcementPage('/login')).toBeNull();
		expect(announcementPage('/p/[slug]')).toBeNull();
		expect(announcementPage(null)).toBeNull();
	});
});

describe('默认导航', () => {
	it('手机底栏最多 5 项（再多放不下）', () => {
		expect(DEFAULT_TABBAR.length).toBeLessThanOrEqual(5);
	});

	it('默认项的 key 不重复，路径都是站内绝对路径', () => {
		for (const items of [DEFAULT_TOP, DEFAULT_TABBAR]) {
			expect(new Set(items.map((i) => i.key)).size).toBe(items.length);
			expect(items.every((i) => i.href.startsWith('/'))).toBe(true);
		}
	});
});
