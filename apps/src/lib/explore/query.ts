/**
 * 探索页的筛选条件 ↔ URL 查询串（docs/04 B.3：筛选写入 URL，可分享、可缓存、可后退）。
 * terms 形如 `<维度slug>:<取值slug>`，逗号分隔；同一维度单选时新值替换旧值，多选时追加。
 */
export const SORTS = ['latest', 'hot', 'recommend', 'random'] as const;
export const WINDOWS = ['all', '7d', '14d', '30d', '90d'] as const;
export type Sort = (typeof SORTS)[number];
export type Window = (typeof WINDOWS)[number];

export type ExploreQuery = {
	terms: string[];
	tag: string;
	finished: '' | 'true' | 'false';
	window: Window;
	sort: Sort;
};

const SLUG = /^[a-z0-9][a-z0-9-]{0,63}$/;
const TERM = /^[a-z0-9][a-z0-9-]{0,63}:[a-z0-9][a-z0-9-]{0,63}$/;

export function parseExplore(params: URLSearchParams): ExploreQuery {
	const sort = params.get('sort') as Sort;
	const window = params.get('window') as Window;
	const finished = params.get('finished');
	const terms = [...new Set((params.get('terms') ?? '').split(',').map((t) => t.trim()).filter((t) => TERM.test(t)))].slice(0, 8);
	const tag = params.get('tag') ?? '';
	return {
		terms,
		tag: SLUG.test(tag) ? tag : '',
		finished: finished === 'true' || finished === 'false' ? finished : '',
		window: WINDOWS.includes(window) ? window : 'all',
		sort: SORTS.includes(sort) ? sort : 'latest'
	};
}

/** 规范化后的查询串（键按字母排序，默认值省略），同一组条件只有一个地址，利于缓存 */
export function exploreSearch(query: ExploreQuery): string {
	const params = new URLSearchParams();
	if (query.finished) {
		params.set('finished', query.finished);
	}
	if (query.sort !== 'latest') {
		params.set('sort', query.sort);
	}
	if (query.tag) {
		params.set('tag', query.tag);
	}
	if (query.terms.length > 0) {
		params.set('terms', [...query.terms].sort().join(','));
	}
	if (query.window !== 'all') {
		params.set('window', query.window);
	}
	const text = params.toString();
	return text ? `?${text}` : '';
}

/** 切换某个维度取值：单选维度替换同维度的旧值，多选维度追加或去掉 */
export function toggleTerm(query: ExploreQuery, taxonomy: string, term: string | null, multi: boolean): ExploreQuery {
	const others = query.terms.filter((t) => !t.startsWith(`${taxonomy}:`));
	if (term === null) {
		return { ...query, terms: others };
	}
	const key = `${taxonomy}:${term}`;
	const selected = query.terms.filter((t) => t.startsWith(`${taxonomy}:`));
	if (selected.includes(key)) {
		return { ...query, terms: [...others, ...selected.filter((t) => t !== key)] };
	}
	return { ...query, terms: multi ? [...others, ...selected, key].slice(0, 8) : [...others, key] };
}

/** 传给后端的参数（不含 cursor / limit） */
export function exploreParams(query: ExploreQuery): Record<string, string> {
	const out: Record<string, string> = { sort: query.sort };
	if (query.terms.length > 0) {
		out.terms = query.terms.join(',');
	}
	if (query.tag) {
		out.tag = query.tag;
	}
	if (query.finished) {
		out.finished = query.finished;
	}
	if (query.window !== 'all') {
		out.window = query.window;
	}
	return out;
}
