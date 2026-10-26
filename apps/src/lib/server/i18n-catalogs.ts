/**
 * 内置文案（构建时打进服务端包）。每次请求只把当前语言的文案随页面数据下发，
 * 客户端包里不会带上其他 8 种语言。
 *
 * 线上文案改为从 CDN 读取后（docs/04 A.4.3），这里仍作为 CDN 不可用时的兜底。
 */
import type { Locale } from '#lib/i18n/locales.ts';
import ar from '#lib/i18n/messages/ar.json';
import en from '#lib/i18n/messages/en.json';
import es from '#lib/i18n/messages/es.json';
import hi from '#lib/i18n/messages/hi.json';
import ja from '#lib/i18n/messages/ja.json';
import ko from '#lib/i18n/messages/ko.json';
import th from '#lib/i18n/messages/th.json';
import vi from '#lib/i18n/messages/vi.json';
import zhCN from '#lib/i18n/messages/zh-CN.json';
import type { Messages } from '#lib/i18n/translate.ts';

// 其他语言的 JSON 与基准语言的键一致由 validateCatalogs 保证（单元测试 + 发布脚本），这里只做类型收窄
export const CATALOGS: Record<Locale, Messages> = {
	'zh-CN': zhCN,
	en: en as Messages,
	es: es as Messages,
	vi: vi as Messages,
	ja: ja as Messages,
	ko: ko as Messages,
	th: th as Messages,
	hi: hi as Messages,
	ar: ar as Messages
};
