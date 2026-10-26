import type zhCN from './messages/zh-CN.json';

/** 文案键以简体中文 JSON 为准：键写错时 svelte-check 报错。 */
export type MessageKey = keyof typeof zhCN;
export type Messages = Record<MessageKey, string>;
export type TranslateParams = Record<string, string | number>;

/** 取文案并替换 {name} 占位符；缺少参数时保留占位符原样，便于发现问题。 */
export function translate(messages: Messages, key: MessageKey, params?: TranslateParams): string {
	const template = messages[key];
	if (!params) {
		return template;
	}
	return template.replace(/\{(\w+)\}/g, (whole, name: string) =>
		name in params ? String(params[name]) : whole
	);
}

/** 业务码 → 当前语言提示；ENC_* 统一提示「安全连接失败」，未知业务码按服务异常处理。 */
export function errorMessage(messages: Messages, code: string): string {
	if (code.startsWith('ENC_')) {
		return messages['error.ENCRYPTION'];
	}
	const key = `error.${code}`;
	return key in messages ? messages[key as MessageKey] : messages['error.INTERNAL'];
}
