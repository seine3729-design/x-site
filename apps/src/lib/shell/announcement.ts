/**
 * 公告的显示频率（docs/07 /api/v1/announcements）：always 每次、once 每人一次、daily 每人每天一次。
 * 记录只存在本机（不含身份信息）；关闭过的顶部公告在本次会话内不再出现。
 */
import type { Announcement, AnnouncementPageKey } from '#lib/api/types.ts';

const SEEN = 'xs_ann_seen';

type SeenMap = Record<string, string>;

function today(): string {
	const d = new Date();
	return `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`;
}

function readSeen(): SeenMap {
	try {
		return JSON.parse(localStorage.getItem(SEEN) ?? '{}') as SeenMap;
	} catch {
		return {};
	}
}

export function markSeen(item: Announcement) {
	try {
		const seen = readSeen();
		seen[item.id] = today();
		// 只留最近 50 条记录，避免无限增长
		const entries = Object.entries(seen).slice(-50);
		localStorage.setItem(SEEN, JSON.stringify(Object.fromEntries(entries)));
		sessionStorage.setItem(`${SEEN}:${item.id}`, '1');
	} catch {
		// 隐私模式：本次照常关闭
	}
}

export function shouldShow(item: Announcement, page: AnnouncementPageKey | null): boolean {
	if (item.pages.length > 0 && (!page || !item.pages.includes(page))) {
		return false;
	}
	try {
		if (sessionStorage.getItem(`${SEEN}:${item.id}`)) {
			return false;
		}
		const last = readSeen()[item.id];
		if (item.frequency === 'once') {
			return !last;
		}
		if (item.frequency === 'daily') {
			return last !== today();
		}
		return true;
	} catch {
		return true;
	}
}

/** 公告跳转地址：站内路径或 http(s)；其他协议一律不跳 */
export function safeHref(value: string): { href: string; external: boolean } | null {
	const url = value.trim();
	if (/^https?:\/\//i.test(url)) {
		return { href: url, external: true };
	}
	if (url.startsWith('/') && !url.startsWith('//')) {
		return { href: url, external: false };
	}
	return null;
}
