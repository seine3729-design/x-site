import { afterEach, describe, expect, it, vi } from 'vitest';
import { CATALOGS } from '#lib/server/i18n-catalogs.ts';
import {
	DEFAULT_LOCALE,
	LOCALES,
	localizePath,
	matchLocale,
	parseAcceptLanguage,
	splitLocale,
	textDirection
} from './locales.ts';
import { suggestedRedirect } from './preference.ts';
import { errorMessage, translate } from './translate.ts';
import { validateCatalogs } from './validate.ts';

describe('语言前缀', () => {
	it('拆分与拼接互为逆运算', () => {
		expect(splitLocale('/en/d/1')).toEqual({ locale: 'en', path: '/d/1', prefixed: true });
		expect(splitLocale('/ar')).toEqual({ locale: 'ar', path: '/', prefixed: true });
		expect(splitLocale('/d/1')).toEqual({ locale: DEFAULT_LOCALE, path: '/d/1', prefixed: false });
		// 不是支持的语言、或只是以语言代码开头的路径，不当作前缀
		expect(splitLocale('/fr/d/1').prefixed).toBe(false);
		expect(splitLocale('/english').prefixed).toBe(false);
		for (const locale of LOCALES) {
			for (const path of ['/', '/play/1/2']) {
				const localized = localizePath(path, locale);
				expect(splitLocale(localized)).toMatchObject({ locale, path });
			}
		}
		expect(localizePath('/', DEFAULT_LOCALE)).toBe('/');
		expect(localizePath('/', 'ja')).toBe('/ja');
	});

	it('只有阿拉伯语从右到左', () => {
		expect(LOCALES.filter((l) => textDirection(l) === 'rtl')).toEqual(['ar']);
	});
});

describe('浏览器语言匹配', () => {
	it('按优先顺序挑第一个支持的语言', () => {
		expect(matchLocale(['fr-FR', 'vi-VN', 'en'])).toBe('vi');
		expect(matchLocale(['ja-JP'])).toBe('ja');
		expect(matchLocale(['pt-BR', 'de'])).toBeNull();
	});

	it('简体中文的各种写法都匹配 zh-CN，繁体不匹配', () => {
		for (const tag of ['zh', 'zh-CN', 'zh-Hans', 'zh-Hans-CN', 'zh-SG']) {
			expect(matchLocale([tag])).toBe('zh-CN');
		}
		expect(matchLocale(['zh-TW', 'en-US'])).toBe('en');
		expect(matchLocale(['zh-Hant-HK'])).toBeNull();
	});

	it('解析 Accept-Language 并按 q 值排序', () => {
		expect(parseAcceptLanguage('en;q=0.5, th-TH, ko;q=0.8, *;q=0.1, de;q=0')).toEqual(['th-TH', 'ko', 'en']);
		expect(parseAcceptLanguage(null)).toEqual([]);
	});
});

describe('文案 JSON', () => {
	const base = CATALOGS[DEFAULT_LOCALE];

	it('9 种语言都通过校验（键齐全、非空、占位符与中文一致）', () => {
		expect(validateCatalogs(base, CATALOGS)).toEqual([]);
	});

	it('校验能发现缺键、多余键、空值与占位符不一致', () => {
		const problems = validateCatalogs(
			{ a: '你好 {name}', b: '再见' },
			{ en: { a: 'Hello {user}', c: 'extra', b: ' ' } }
		);
		expect(problems).toEqual([
			'en: a 占位符不一致（应为 {name}，实际 {user}）',
			'en: 多余的键 c（基准语言中没有）',
			'en: b 必须是非空字符串'
		]);
		expect(validateCatalogs({ a: 'x' }, { ja: {} })).toEqual(['ja: 缺少 a']);
	});

	it('非中文语言不应原样照抄中文', () => {
		for (const locale of LOCALES.filter((l) => l !== DEFAULT_LOCALE)) {
			const copied = Object.entries(CATALOGS[locale]).filter(([k, v]) => v === base[k as keyof typeof base]);
			expect(copied, locale).toEqual([]);
		}
	});

	it('替换占位符，业务码映射到当前语言', () => {
		const messages = { ...CATALOGS.en, 'nav.home': 'Hi {name}, {n}' };
		expect(translate(messages, 'nav.home', { name: 'A', n: 2 })).toBe('Hi A, 2');
		expect(translate(messages, 'nav.home', { name: 'A' })).toBe('Hi A, {n}');
		expect(errorMessage(CATALOGS.ja, 'NEED_LOGIN')).toBe(CATALOGS.ja['error.NEED_LOGIN']);
		expect(errorMessage(CATALOGS.ar, 'ENC_TIME_SKEW')).toBe(CATALOGS.ar['error.ENCRYPTION']);
		expect(errorMessage(CATALOGS.ko, 'SOMETHING_NEW')).toBe(CATALOGS.ko['error.INTERNAL']);
	});
});

describe('首次访问按浏览器语言跳转', () => {
	const store = new Map<string, string>();
	vi.stubGlobal('localStorage', {
		getItem: (k: string) => store.get(k) ?? null,
		setItem: (k: string, v: string) => store.set(k, v)
	});
	afterEach(() => store.clear());

	const loc = (pathname: string) => ({ pathname, search: '?a=1', hash: '#x' });
	const UA = 'Mozilla/5.0 (iPhone)';

	it('无前缀地址按浏览器语言跳到对应前缀，保留查询串与锚点', () => {
		expect(suggestedRedirect(loc('/d/1'), ['th-TH'], UA)).toBe('/th/d/1?a=1#x');
	});

	it('浏览器是中文、地址已带前缀、爬虫时不跳', () => {
		expect(suggestedRedirect(loc('/d/1'), ['zh-CN'], UA)).toBeNull();
		expect(suggestedRedirect(loc('/en/d/1'), ['ja'], UA)).toBeNull();
		expect(suggestedRedirect(loc('/d/1'), ['en'], 'Googlebot/2.1')).toBeNull();
	});

	it('用户选过的语言优先于浏览器语言（选中文则不跳）', () => {
		store.set('lang_pref', 'zh-CN');
		expect(suggestedRedirect(loc('/'), ['en'], UA)).toBeNull();
		store.set('lang_pref', 'ko');
		expect(suggestedRedirect(loc('/'), ['en'], UA)).toBe('/ko?a=1#x');
	});
});
