/** 本机搜索历史（docs/04 B.9）：最多 12 条，最近的在前，可清空；不含身份信息。 */
const KEY = 'xs_search_history';
const LIMIT = 12;

export function readSearchHistory(): string[] {
	try {
		const list = JSON.parse(localStorage.getItem(KEY) ?? '[]') as unknown;
		return Array.isArray(list) ? list.filter((v): v is string => typeof v === 'string').slice(0, LIMIT) : [];
	} catch {
		return [];
	}
}

export function addSearchHistory(query: string): string[] {
	const value = query.trim();
	if (!value) {
		return readSearchHistory();
	}
	const next = [value, ...readSearchHistory().filter((v) => v !== value)].slice(0, LIMIT);
	try {
		localStorage.setItem(KEY, JSON.stringify(next));
	} catch {
		// 写不进去时只是不记历史
	}
	return next;
}

export function clearSearchHistory() {
	try {
		localStorage.removeItem(KEY);
	} catch {
		// 忽略
	}
}
