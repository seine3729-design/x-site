import { callContext } from '#lib/server/context.ts';
import { orFallback, pageError, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

/** 取封面的专题个数上限：首屏之外的专题退回渐变底，不为了配图把一页变成几十个请求 */
const DETAIL_LIMIT = 12;

export const load: PageServerLoad = async (event) => {
	const ctx = callContext(event);
	try {
		const topics = await publicApi.topics(ctx, { limit: 30 });
		// 专题列表接口（/api/v1/topics）只有标题与简介，没有封面；按 docs/04 A.3「BFF 做聚合」
		// 在这里并发取详情拿前几部剧的封面拼图。详情本身在 BFF 里缓存 60 秒，同一页反复打开不会重复回源。
		const details = await Promise.all(
			topics.list.slice(0, DETAIL_LIMIT).map((t) => orFallback(publicApi.topic(ctx, t.slug), null))
		);
		const covers: Record<string, { covers: string[]; count: number }> = {};
		details.forEach((detail, index) => {
			const slug = topics.list[index]?.slug;
			if (!slug || !detail) {
				return;
			}
			covers[slug] = {
				covers: detail.dramas.map((d) => d.cover_portrait).filter(Boolean).slice(0, 3),
				count: detail.dramas.length
			};
		});
		return { topics, covers };
	} catch (err) {
		pageError(err);
	}
};
