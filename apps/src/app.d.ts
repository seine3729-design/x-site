declare global {
	namespace App {
		interface Locals {
			requestId: string;
			country: string;
			/** 当前页面语言（URL 前缀决定，见 #lib/i18n/locales.ts） */
			locale: import('#lib/i18n/locales.ts').Locale;
		}
		interface Error {
			message: string;
			/** 业务码（docs/07 第 4 节），错误页按它显示当前语言的提示 */
			code?: string;
		}
	}
}

export {};
