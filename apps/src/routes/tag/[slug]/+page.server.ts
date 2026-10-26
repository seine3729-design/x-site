import { redirect } from '@sveltejs/kit';
import { callContext } from '#lib/server/context.ts';
import { pageError, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

const SORTS = ['latest', 'hot', 'recommend'] as const;

export const load: PageServerLoad = async (event) => {
	const sortParam = event.url.searchParams.get('sort') as (typeof SORTS)[number];
	const sort = SORTS.includes(sortParam) ? sortParam : 'latest';
	let result;
	try {
		result = await publicApi.tag(callContext(event), event.params.slug, { sort, limit: 24 });
	} catch (err) {
		pageError(err);
	}
	// 已合并的标签跳到目标标签（保留语言前缀与排序）
	if (result.tag.canonical_slug && result.tag.canonical_slug !== event.params.slug) {
		const prefix = event.url.pathname.slice(0, event.url.pathname.indexOf('/tag/'));
		redirect(301, `${prefix}/tag/${encodeURIComponent(result.tag.canonical_slug)}${event.url.search}`);
	}
	return { result, sort };
};
