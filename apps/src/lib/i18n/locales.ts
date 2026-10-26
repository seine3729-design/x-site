/**
 * 前台支持的语言（docs/04 A.4）。运营后台只用中文，不读这里。
 *
 * URL 规则：默认语言（简体中文）不带前缀，其他语言带前缀，如 /en/d/1、/ar/play/1/2。
 * 页面路由只写一份，由 src/hooks.ts 的 reroute 去掉前缀后匹配；/api/* 不带语言前缀。
 */
export const LOCALES = ['zh-CN', 'en', 'es', 'vi', 'ja', 'ko', 'th', 'hi', 'ar'] as const;
export type Locale = (typeof LOCALES)[number];

export const DEFAULT_LOCALE: Locale = 'zh-CN';

/** URL 前缀（默认语言没有前缀）。 */
const PREFIX: Record<Locale, string> = {
	'zh-CN': '',
	en: 'en',
	es: 'es',
	vi: 'vi',
	ja: 'ja',
	ko: 'ko',
	th: 'th',
	hi: 'hi',
	ar: 'ar'
};

/** 语言菜单里显示的本族语名称（不随当前语言翻译，方便用户认出自己的语言）。 */
export const LOCALE_NAMES: Record<Locale, string> = {
	'zh-CN': '简体中文',
	en: 'English',
	es: 'Español',
	vi: 'Tiếng Việt',
	ja: '日本語',
	ko: '한국어',
	th: 'ไทย',
	hi: 'हिन्दी',
	ar: 'العربية'
};

const RTL = new Set<Locale>(['ar']);

const BY_PREFIX = new Map<string, Locale>(
	LOCALES.filter((l) => PREFIX[l]).map((l) => [PREFIX[l], l] as const)
);

export function isLocale(value: string): value is Locale {
	return (LOCALES as readonly string[]).includes(value);
}

export function textDirection(locale: Locale): 'ltr' | 'rtl' {
	return RTL.has(locale) ? 'rtl' : 'ltr';
}

/** 从路径里拆出语言前缀：`/en/d/1` → { locale: 'en', path: '/d/1' }；无前缀时为默认语言。 */
export function splitLocale(pathname: string): { locale: Locale; path: string; prefixed: boolean } {
	const match = /^\/([a-z]{2})(?=\/|$)/.exec(pathname);
	const locale = match ? BY_PREFIX.get(match[1]!) : undefined;
	if (!match || !locale) {
		return { locale: DEFAULT_LOCALE, path: pathname, prefixed: false };
	}
	return { locale, path: pathname.slice(match[0].length) || '/', prefixed: true };
}

/** 给站内路径加上语言前缀。path 必须以 / 开头且不带语言前缀。 */
export function localizePath(path: string, locale: Locale): string {
	const prefix = PREFIX[locale];
	if (!prefix) {
		return path;
	}
	return path === '/' ? `/${prefix}` : `/${prefix}${path}`;
}

/**
 * 按浏览器语言列表（navigator.languages / Accept-Language 顺序）挑选最合适的支持语言。
 * 只认中文简体（zh、zh-CN、zh-Hans、zh-SG），繁体中文没有对应翻译时不匹配中文以外的语言。
 */
export function matchLocale(preferred: readonly string[]): Locale | null {
	for (const raw of preferred) {
		const tag = raw.trim().toLowerCase();
		if (!tag) {
			continue;
		}
		const [language, ...rest] = tag.split('-');
		if (language === 'zh') {
			const traditional = rest.some((part) => part === 'hant' || part === 'tw' || part === 'hk' || part === 'mo');
			if (!traditional) {
				return 'zh-CN';
			}
			continue;
		}
		const hit = LOCALES.find((l) => l === language);
		if (hit) {
			return hit;
		}
	}
	return null;
}

/** 解析 Accept-Language 头，按 q 值从高到低返回语言标签。 */
export function parseAcceptLanguage(header: string | null): string[] {
	if (!header) {
		return [];
	}
	return header
		.split(',')
		.map((part) => {
			const [tag = '', ...params] = part.trim().split(';');
			const q = params.map((p) => p.trim()).find((p) => p.startsWith('q='));
			return { tag: tag.trim(), q: q ? Number(q.slice(2)) : 1 };
		})
		.filter((item) => item.tag && item.tag !== '*' && Number.isFinite(item.q) && item.q > 0)
		.sort((a, b) => b.q - a.q)
		.map((item) => item.tag);
}
