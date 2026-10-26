import type { CursorPage, DramaCard } from '#lib/api/types.ts';
import { callContext } from '#lib/server/context.ts';
import { cachedGet, orFallback, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

/**
 * 刷剧流（docs/04 B.7）：GET /api/v1/feed/shua 预留（APP 与 Web 共用，后端未实现时 404），
 * 没有时退回探索接口按热度排序——同样是「从最热开始」，只是还没有个性化与整屏广告。
 */
export const load: PageServerLoad = async (event) => {
	const ctx = callContext(event);
	const empty: CursorPage<DramaCard> = { list: [], next_cursor: null, total: 0 };
	const feed = await orFallback<CursorPage<DramaCard> | null>(cachedGet(ctx, '/api/v1/feed/shua', 30_000), null);
	if (feed && feed.list.length > 0) {
		return { source: 'feed' as const, page: feed };
	}
	return { source: 'explore' as const, page: await orFallback(publicApi.explore(ctx, { sort: 'hot', limit: 12 }), empty) };
};
