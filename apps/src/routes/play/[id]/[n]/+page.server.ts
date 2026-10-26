import { error } from '@sveltejs/kit';
import { callContext } from '#lib/server/context.ts';
import { pageError, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

/**
 * 播放页 SSR 只带公共数据（剧与分集列表，可进边缘缓存）；选源、观看权限、续播位置都在浏览器里经加密接口取。
 */
export const load: PageServerLoad = async (event) => {
	const id = Number(event.params.id);
	const number = Number(event.params.n);
	if (!Number.isSafeInteger(id) || id <= 0 || !Number.isSafeInteger(number) || number <= 0) {
		error(404, { message: 'NOT_FOUND', code: 'NOT_FOUND' });
	}
	let drama;
	try {
		drama = await publicApi.drama(callContext(event), id);
	} catch (err) {
		pageError(err);
	}
	const episode = drama.episodes.find((e) => e.number === number);
	if (!episode) {
		error(404, { message: 'NOT_FOUND', code: 'NOT_FOUND' });
	}
	return { drama, episode };
};
