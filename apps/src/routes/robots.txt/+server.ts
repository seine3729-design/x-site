import type { RequestHandler } from './$types';

export const GET: RequestHandler = () => {
	return new Response('User-agent: *\nAllow: /\nDisallow: /api/\n', {
		headers: {
			'content-type': 'text/plain; charset=utf-8',
			'cache-control': 'public, max-age=3600'
		}
	});
};
