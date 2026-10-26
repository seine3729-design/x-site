import { getContext, setContext } from 'svelte';
import { localizePath, textDirection, type Locale } from './locales.ts';
import { errorMessage, translate, type MessageKey, type Messages, type TranslateParams } from './translate.ts';

export type I18nData = { locale: Locale; messages: Messages };

export type I18n = {
	readonly locale: Locale;
	readonly dir: 'ltr' | 'rtl';
	t(key: MessageKey, params?: TranslateParams): string;
	/** 业务码 → 当前语言提示 */
	error(code: string): string;
	/** 站内路径加上当前语言前缀 */
	path(path: string): string;
};

const KEY = Symbol('i18n');

/** 在根 layout 调用。source 返回 layout 数据里的 i18n，取值处会随数据更新。 */
export function setI18n(source: () => I18nData): I18n {
	const i18n: I18n = {
		get locale() {
			return source().locale;
		},
		get dir() {
			return textDirection(source().locale);
		},
		t: (key, params) => translate(source().messages, key, params),
		error: (code) => errorMessage(source().messages, code),
		path: (path) => localizePath(path, source().locale)
	};
	return setContext(KEY, i18n);
}

export function getI18n(): I18n {
	const i18n = getContext<I18n | undefined>(KEY);
	if (!i18n) {
		throw new Error('getI18n() 只能在根 layout 之下的组件中调用');
	}
	return i18n;
}
