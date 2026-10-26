/**
 * 站点公共配置在组件间共享（根 layout 设置一次）。数据来自 /api/v1/site-config 的公共部分，
 * 会进入可被边缘缓存的页面，不含任何个人信息。
 */
import { getContext, setContext } from 'svelte';
import type { AgeGate, Announcement, PublicMenu, SiteAddresses } from '#lib/api/types.ts';
import type { LayoutSettings, ThemeSettings } from '#lib/theme/settings.ts';
import type { IconName } from '#lib/ui/icons.ts';

export type SiteData = {
	version: number;
	name: string;
	slogan: string;
	/** 头部 Logo 的图片 key 或完整地址（后台 site.logo_light），留空用站名文字 */
	logo: string;
	favicon: string;
	/** 页脚版权文字，留空用文案模板 */
	copyright: string;
	features: Record<string, boolean>;
	ageGate: AgeGate;
	/** 配色与观感：颜色已由 SSR 注入 CSS 变量，这里是组件要用的封面比例、深浅色开关等 */
	theme: ThemeSettings;
	layout: LayoutSettings;
	menus: PublicMenu[];
	footerPages: { slug: string; title: string }[];
	addresses: SiteAddresses | null;
	imageBases: string[];
	announcements: Announcement[];
};

const KEY = Symbol('site');

export function setSite(source: () => SiteData): () => SiteData {
	setContext(KEY, source);
	return source;
}

export function getSite(): SiteData {
	const source = getContext<(() => SiteData) | undefined>(KEY);
	if (!source) {
		throw new Error('getSite() 只能在根 layout 之下的组件中调用');
	}
	return source();
}

/** 功能开关：后台没设置的按打开处理，只有明确 false 才算关闭（docs/07 功能开关）。 */
export function featureOn(features: Record<string, boolean>, name: string): boolean {
	return features[name] !== false;
}

/** 站内页白名单（MenuLinkType=internal 的 link_value）→ 前台路径 */
const INTERNAL: Record<string, string> = {
	home: '/',
	shua: '/shua',
	explore: '/explore',
	rank: '/rank',
	topic: '/topics',
	search: '/search',
	me: '/me',
	way_home: '/way-home'
};

export type NavItem = {
	key: string;
	label: string;
	/** 站内路径（未加语言前缀）或外部 http(s) 地址 */
	href: string;
	external: boolean;
	icon: IconName | string;
	badge: string;
	device: PublicMenu['device'];
	visibleTo: PublicMenu['visible_to'];
	newTab: boolean;
	group: string;
};

export function menuHref(menu: Pick<PublicMenu, 'link_type' | 'link_value'>): { href: string; external: boolean } | null {
	const value = menu.link_value.trim();
	switch (menu.link_type) {
		case 'internal':
			return INTERNAL[value] ? { href: INTERNAL[value], external: false } : null;
		case 'page':
			return { href: `/p/${encodeURIComponent(value)}`, external: false };
		case 'channel':
			return { href: `/c/${encodeURIComponent(value)}`, external: false };
		case 'topic':
			return { href: `/topics/${encodeURIComponent(value)}`, external: false };
		case 'tag':
			return { href: `/tag/${encodeURIComponent(value)}`, external: false };
		case 'external':
			return /^https?:\/\//i.test(value) ? { href: value, external: true } : null;
		default:
			return null;
	}
}

export function toNavItems(menus: PublicMenu[], position: PublicMenu['position']): NavItem[] {
	const items: NavItem[] = [];
	for (const menu of menus) {
		if (menu.position !== position) {
			continue;
		}
		const link = menuHref(menu);
		if (!link) {
			continue;
		}
		items.push({
			key: `m${menu.id}`,
			label: menu.label,
			href: link.href,
			external: link.external,
			icon: (position === 'mobile_tabbar' && menu.tabbar_icon) || menu.icon,
			badge: menu.badge,
			device: menu.device,
			visibleTo: menu.visible_to,
			newTab: menu.new_tab,
			group: menu.group_title
		});
	}
	return items;
}
