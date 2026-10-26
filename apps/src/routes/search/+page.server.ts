import type { SearchPage } from '#lib/api/types.ts';
import { callContext } from '#lib/server/context.ts';
import { orFallback, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async (event) => {
	const ctx = callContext(event);
	const q = (event.url.searchParams.get('q') ?? '').trim().slice(0, 50);
	const empty: SearchPage = { query: q, list: [], next_cursor: null, total: 0 };
	// 没有关键词时才需要「热门影片」：用周榜的真实数据填首屏，有关键词时不白拉一次
	const [hot, hotDramas, result] = await Promise.all([
		orFallback(publicApi.hotWords(ctx), []),
		q ? Promise.resolve([]) : orFallback(publicApi.rank(ctx, 'drama_week').then((b) => b.list.slice(0, 10)), []),
		q ? orFallback(publicApi.search(ctx, { q, limit: 24 }), empty) : Promise.resolve(null)
	]);
	return { q, hot, hotDramas, result };
};
