import { exploreParams, parseExplore } from '#lib/explore/query.ts';
import { callContext } from '#lib/server/context.ts';
import { pageError, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async (event) => {
	const query = parseExplore(event.url.searchParams);
	try {
		const result = await publicApi.explore(callContext(event), { ...exploreParams(query), limit: 24 });
		// 分面只在第一页返回；带筛选的页面也要显示全部维度，所以单独取一次不带条件的第一页的分面
		const facets =
			result.facets ??
			(await publicApi.explore(callContext(event), { sort: 'latest', limit: 1 }).then((r) => r.facets)) ?? { taxonomies: [] };
		return { query, result, facets };
	} catch (err) {
		pageError(err);
	}
};
