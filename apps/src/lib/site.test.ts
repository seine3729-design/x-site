import { describe, expect, it } from 'vitest';
import type { PublicMenu } from '#lib/api/types.ts';
import { featureOn, menuHref, toNavItems } from './site.ts';

const menu = (over: Partial<PublicMenu> = {}): PublicMenu =>
	({
		id: 1,
		position: 'top',
		label: '首页',
		link_type: 'internal',
		link_value: 'home',
		icon: '',
		tabbar_icon: '',
		badge: '',
		device: 'all',
		visible_to: 'all',
		new_tab: false,
		group_title: '',
		...over
	}) as PublicMenu;

describe('featureOn', () => {
	it('没配过的开关按打开处理，只有明确 false 才算关', () => {
		expect(featureOn({}, 'shua')).toBe(true);
		expect(featureOn({ shua: true }, 'shua')).toBe(true);
		expect(featureOn({ shua: false }, 'shua')).toBe(false);
	});
});

describe('menuHref', () => {
	it('站内页只认白名单里的值', () => {
		expect(menuHref({ link_type: 'internal', link_value: 'home' })).toEqual({ href: '/', external: false });
		expect(menuHref({ link_type: 'internal', link_value: 'way_home' })).toEqual({ href: '/way-home', external: false });
		expect(menuHref({ link_type: 'internal', link_value: 'admin' })).toBeNull();
	});

	it('频道、专题、标签、静态页按各自路径拼，取值做转义', () => {
		expect(menuHref({ link_type: 'channel', link_value: 'sweet' })).toEqual({ href: '/c/sweet', external: false });
		expect(menuHref({ link_type: 'topic', link_value: 'hot-2026' })).toEqual({ href: '/topics/hot-2026', external: false });
		expect(menuHref({ link_type: 'tag', link_value: 'ceo' })).toEqual({ href: '/tag/ceo', external: false });
		expect(menuHref({ link_type: 'page', link_value: 'about us' })).toEqual({ href: '/p/about%20us', external: false });
	});

	it('外链必须是 http(s)，否则丢掉（防止 javascript: 菜单）', () => {
		expect(menuHref({ link_type: 'external', link_value: 'https://x.example' })).toEqual({
			href: 'https://x.example',
			external: true
		});
		expect(menuHref({ link_type: 'external', link_value: 'javascript:alert(1)' })).toBeNull();
		expect(menuHref({ link_type: 'external', link_value: '/explore' })).toBeNull();
	});

	it('取值两端的空格不算内容', () => {
		expect(menuHref({ link_type: 'internal', link_value: '  home  ' })).toEqual({ href: '/', external: false });
		expect(menuHref({ link_type: 'external', link_value: '   ' })).toBeNull();
	});
});

describe('toNavItems', () => {
	it('只取该位置的菜单，链接非法的直接跳过', () => {
		const items = toNavItems(
			[
				menu({ id: 1, position: 'top', label: '首页' }),
				menu({ id: 2, position: 'mobile_tabbar', label: '我的', link_value: 'me' }),
				menu({ id: 3, position: 'top', label: '坏链接', link_type: 'external', link_value: 'ftp://x' })
			],
			'top'
		);
		expect(items.map((i) => i.label)).toEqual(['首页']);
		expect(items[0]!.key).toBe('m1');
	});

	it('底栏优先用底栏图标，没填则用通用图标', () => {
		const [withTab, withoutTab] = toNavItems(
			[
				menu({ id: 1, position: 'mobile_tabbar', icon: 'house', tabbar_icon: 'compass', link_value: 'home' }),
				menu({ id: 2, position: 'mobile_tabbar', icon: 'user', tabbar_icon: '', link_value: 'me' })
			],
			'mobile_tabbar'
		);
		expect(withTab!.icon).toBe('compass');
		expect(withoutTab!.icon).toBe('user');
	});
});
