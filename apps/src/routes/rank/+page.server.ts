import type { RankBoard } from '#lib/api/types.ts';
import { callContext } from '#lib/server/context.ts';
import { ServiceError } from '#lib/server/api.ts';
import { pageError, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async (event) => {
	const board = event.url.searchParams.get('board') === 'drama_month' ? 'drama_month' : 'drama_week';
	try {
		return { board, rank: await publicApi.rank(callContext(event), board), disabled: false };
	} catch (err) {
		// 后台关闭了排行榜（features.ranks=false）时接口 404：页面照常打开，显示「暂未开放」
		if (err instanceof ServiceError && err.status === 404) {
			const empty: RankBoard = { board, computed_at: null, list: [] };
			return { board, rank: empty, disabled: true };
		}
		pageError(err);
	}
};
