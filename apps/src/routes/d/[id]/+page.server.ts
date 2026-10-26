import { error } from '@sveltejs/kit';
import { callContext } from '#lib/server/context.ts';
import { pageError, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async (event) => {
	const id = Number(event.params.id);
	if (!Number.isSafeInteger(id) || id <= 0) {
		error(404, { message: 'NOT_FOUND', code: 'NOT_FOUND' });
	}
	try {
		return { drama: await publicApi.drama(callContext(event), id) };
	} catch (err) {
		pageError(err);
	}
};
