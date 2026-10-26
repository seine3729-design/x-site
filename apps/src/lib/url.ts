/** 拼查询串：跳过空值，键按给定顺序；结果带 `?`，没有参数时为空串。 */
export function searchString(params: Record<string, string | number | null | undefined>): string {
	const search = new URLSearchParams();
	for (const [key, value] of Object.entries(params)) {
		if (value !== undefined && value !== null && value !== '') {
			search.set(key, String(value));
		}
	}
	const text = search.toString();
	return text ? `?${text}` : '';
}
