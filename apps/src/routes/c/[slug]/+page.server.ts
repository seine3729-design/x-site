import { callContext } from '#lib/server/context.ts';
import { orFallback, pageError, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

const SORTS = ['latest', 'hot', 'recommend'] as const;
const PAGE_SIZE = 24;

export const load: PageServerLoad = async (event) => {
	const ctx = callContext(event);
	const { slug } = event.params;
	const sortParam = event.url.searchParams.get('sort') as (typeof SORTS)[number];
	const sort = SORTS.includes(sortParam) ? sortParam : 'latest';
	const pageNumber = Math.min(100, Math.max(1, Math.floor(Number(event.url.searchParams.get('page')) || 1)));
	try {
		const [list, blocks] = await Promise.all([
			publicApi.channel(ctx, slug, { sort, page: pageNumber, page_size: PAGE_SIZE }),
			// 频道页也可以由后台编排区块（channel:{slug}），没有编排时只显示列表
			pageNumber === 1 && sort === 'latest'
				? orFallback(publicApi.home(ctx, `channel:${slug}`), { page: '', blocks: [] })
				: Promise.resolve({ page: '', blocks: [] })
		]);
		return { list, blocks: blocks.blocks, sort, page: pageNumber, pageSize: PAGE_SIZE };
	} catch (err) {
		pageError(err);
	}
};
