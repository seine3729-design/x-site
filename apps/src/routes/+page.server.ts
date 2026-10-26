import type { HomeBlock } from '#lib/api/types.ts';
import { callContext } from '#lib/server/context.ts';
import { orFallback, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

/**
 * 首页由后台「页面区块」编排（S3-1）。后台还没编排任何区块时，用探索与榜单接口拼一个默认首页
 * （轮播 + 热播榜 + 热门 + 最新），站点刚上线时不至于空白；有一部可见的剧都没有时才显示空状态。
 */
export const load: PageServerLoad = async (event) => {
	const ctx = callContext(event);
	const home = await orFallback(publicApi.home(ctx, 'home'), { page: 'home', blocks: [] });
	if (home.blocks.length > 0) {
		return { blocks: home.blocks, fallback: false };
	}

	const empty = { list: [], next_cursor: null, total: 0 };
	const [hot, latest, rank] = await Promise.all([
		orFallback(publicApi.explore(ctx, { sort: 'hot', limit: 18 }), empty),
		orFallback(publicApi.explore(ctx, { sort: 'latest', limit: 24 }), empty),
		orFallback(publicApi.rank(ctx, 'drama_week'), { board: 'drama_week' as const, computed_at: null, list: [] })
	]);
	const base = { subtitle: '', more_link: '', cols_desktop: 6, device: 'all' as const };
	const blocks: HomeBlock[] = [];
	if (hot.list.length > 0) {
		blocks.push({ ...base, id: -1, type: 'carousel', title: '', dramas: hot.list.slice(0, 6) });
	}
	if (rank.list.length >= 3) {
		blocks.push({ ...base, id: -2, type: 'rank', title: '', rank_board: 'drama_week', dramas: rank.list.map((r) => r.drama) });
	}
	if (hot.list.length > 6) {
		blocks.push({ ...base, id: -3, type: 'h_scroll', title: '', more_link: '/explore?sort=hot', dramas: hot.list.slice(6) });
	}
	if (latest.list.length > 0) {
		blocks.push({ ...base, id: -4, type: 'poster_grid', title: '', more_link: '/explore', dramas: latest.list });
	}
	return { blocks, fallback: true };
};
