import { APP_ENV } from '$app/env/private';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = ({ locals }) => {
	return Response.json(
		{
			ok: true,
			country: locals.country,
			env: APP_ENV,
			now: Date.now()
		},
		{
			headers: {
				'cache-control': 'no-store'
			}
		}
	);
};
