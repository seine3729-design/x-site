import type { RequestEvent } from '@sveltejs/kit';

import type { CallContext } from '#lib/server/api.ts';

/** 从 SvelteKit 请求事件中取出调用后端所需的上下文。 */
export function callContext(event: Pick<RequestEvent, 'locals' | 'request' | 'getClientAddress'>): CallContext {
	return {
		requestId: event.locals.requestId,
		clientIp: event.getClientAddress(),
		userAgent: event.request.headers.get('user-agent')
	};
}
