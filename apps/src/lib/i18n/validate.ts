/**
 * 文案 JSON 校验：单元测试与发布脚本（scripts/i18n-build.ts）共用。
 * JSON 没有类型检查，译者改错键名、漏掉占位符或留空值时在这里拦下。
 */
export type Catalog = Record<string, unknown>;

const PLACEHOLDER = /\{(\w+)\}/g;

function placeholders(value: string): string[] {
	return [...value.matchAll(PLACEHOLDER)].map((m) => m[1]!).sort();
}

/** 以基准语言为准逐条比对，返回问题列表；为空表示通过。 */
export function validateCatalogs(base: Catalog, catalogs: Record<string, Catalog>): string[] {
	const problems: string[] = [];
	const baseKeys = Object.keys(base);
	for (const [locale, catalog] of Object.entries(catalogs)) {
		for (const key of baseKeys) {
			if (!(key in catalog)) {
				problems.push(`${locale}: 缺少 ${key}`);
			}
		}
		for (const [key, value] of Object.entries(catalog)) {
			if (!(key in base)) {
				problems.push(`${locale}: 多余的键 ${key}（基准语言中没有）`);
				continue;
			}
			if (typeof value !== 'string' || value.trim() === '') {
				problems.push(`${locale}: ${key} 必须是非空字符串`);
				continue;
			}
			const want = placeholders(String(base[key])).join(',');
			const got = placeholders(value).join(',');
			if (want !== got) {
				problems.push(`${locale}: ${key} 占位符不一致（应为 {${want}}，实际 {${got}}）`);
			}
		}
	}
	return problems;
}
