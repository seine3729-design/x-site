/**
 * 导航：后台没配菜单时的默认项、当前位置判断、路由与公告「出现页面」的对应关系。
 */
import type { AnnouncementPageKey } from '#lib/api/types.ts';
import type { MessageKey } from '#lib/i18n/translate.ts';
import type { IconName } from '#lib/ui/icons.ts';

export type DefaultNav = { key: string; labelKey: MessageKey; href: string; icon: IconName };

/** 电脑顶部导航（后台「顶部导航」为空时） */
export const DEFAULT_TOP: DefaultNav[] = [
	{ key: 'home', labelKey: 'nav.home', href: '/', icon: 'house' },
	{ key: 'shua', labelKey: 'nav.shua', href: '/shua', icon: 'clapperboard' },
	{ key: 'explore', labelKey: 'nav.explore', href: '/explore', icon: 'compass' },
	{ key: 'rank', labelKey: 'nav.rank', href: '/rank', icon: 'trophy' },
	{ key: 'topics', labelKey: 'nav.topics', href: '/topics', icon: 'layers' }
];

/** 手机底栏（后台「手机底栏」为空时，最多 5 项） */
export const DEFAULT_TABBAR: DefaultNav[] = [
	{ key: 'home', labelKey: 'nav.home', href: '/', icon: 'house' },
	{ key: 'shua', labelKey: 'nav.shua', href: '/shua', icon: 'clapperboard' },
	{ key: 'explore', labelKey: 'nav.explore', href: '/explore', icon: 'compass' },
	{ key: 'rank', labelKey: 'nav.rank', href: '/rank', icon: 'trophy' },
	{ key: 'me', labelKey: 'nav.me', href: '/me', icon: 'user' }
];

/** 当前路径（不含语言前缀）是否属于该导航项：首页精确匹配，其余按路径前缀 */
export function isActive(href: string, path: string): boolean {
	if (href === '/') {
		return path === '/';
	}
	return path === href || path.startsWith(`${href}/`);
}

/** 沉浸式页面：手机上不显示顶栏与页脚，底栏保留（播放页也能直接切换到别的入口） */
export function immersiveRoute(routeId: string | null): { header: boolean; tabbar: boolean; footer: boolean } {
	if (routeId === '/play/[id]/[n]') {
		return { header: false, tabbar: true, footer: false };
	}
	if (routeId === '/shua') {
		return { header: false, tabbar: true, footer: false };
	}
	return { header: true, tabbar: true, footer: true };
}

const ANNOUNCE_PAGES: Record<string, AnnouncementPageKey> = {
	'/': 'home',
	'/explore': 'explore',
	'/c/[slug]': 'channel',
	'/tag/[slug]': 'tag',
	'/topics': 'topic',
	'/topics/[slug]': 'topic',
	'/d/[id]': 'drama',
	'/play/[id]/[n]': 'play',
	'/shua': 'shua',
	'/rank': 'rank',
	'/search': 'search'
};

export function announcementPage(routeId: string | null): AnnouncementPageKey | null {
	if (!routeId) {
		return null;
	}
	if (routeId === '/me' || routeId.startsWith('/me/')) {
		return 'me';
	}
	return ANNOUNCE_PAGES[routeId] ?? null;
}
