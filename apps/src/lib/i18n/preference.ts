import { DEFAULT_LOCALE, isLocale, localizePath, matchLocale, splitLocale, type Locale } from './locales.ts';

/** 本机语言偏好（不含身份信息，允许放 localStorage）。 */
const STORAGE_KEY = 'lang_pref';
const BOT = /bot|crawl|spider|slurp|lighthouse/i;

export function rememberLocale(locale: Locale) {
	try {
		localStorage.setItem(STORAGE_KEY, locale);
	} catch {
		// 隐私模式等场景写不进去，不影响跳转
	}
}

function storedLocale(): Locale | null {
	try {
		const value = localStorage.getItem(STORAGE_KEY);
		return value && isLocale(value) ? value : null;
	} catch {
		return null;
	}
}

/**
 * 首次访问不带语言前缀的地址时，按浏览器语言跳到对应语言页面（只在客户端执行，不影响边缘缓存）。
 * 用户手动选过语言、地址已带前缀、或是爬虫时不跳。返回要跳转的地址，不需要跳转时返回 null。
 */
export function suggestedRedirect(
	location: Pick<Location, 'pathname' | 'search' | 'hash'>,
	languages: readonly string[],
	userAgent: string
): string | null {
	if (BOT.test(userAgent)) {
		return null;
	}
	const current = splitLocale(location.pathname);
	if (current.prefixed) {
		return null;
	}
	const target = storedLocale() ?? matchLocale(languages);
	if (!target || target === DEFAULT_LOCALE) {
		return null;
	}
	return localizePath(current.path, target) + location.search + location.hash;
}
