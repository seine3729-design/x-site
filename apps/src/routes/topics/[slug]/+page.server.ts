import { callContext } from '#lib/server/context.ts';
import { pageError, publicApi } from '#lib/server/public-api.ts';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async (event) => {
	try {
		return { topic: await publicApi.topic(callContext(event), event.params.slug) };
	} catch (err) {
		pageError(err);
	}
};
