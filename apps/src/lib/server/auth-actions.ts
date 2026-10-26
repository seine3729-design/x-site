/**
 * 登录、注册的表单动作共用逻辑（docs/04 B.10、A.3）：BFF 经内网调用后端，成功后把令牌写进 HttpOnly Cookie。
 * 浏览器提交的是普通表单（无 JS 也能用）；渐进增强后同样提交到这里，令牌始终不经过页面脚本。
 */
import { fail, type RequestEvent } from '@sveltejs/kit';
import type { AuthChallenge } from '#lib/api/types.ts';
import { callService, ServiceError } from '#lib/server/api.ts';
import { callContext } from '#lib/server/context.ts';
import { writeSession, type SessionTokens } from '#lib/server/session.ts';

type AuthAccount = Omit<SessionTokens, 'role'> & { role: 'user'; user: { id: number; nickname: string } };

export type AuthFailure = { code: string; username: string; challenge?: AuthChallenge; field?: 'username' | 'password' | 'ack' };

/** 调用后端登录 / 注册接口；成功返回 null（Cookie 已写好），失败返回表单错误 */
export async function authenticate(
	event: RequestEvent,
	path: '/api/v1/auth/login' | '/api/v1/auth/register',
	body: Record<string, unknown>,
	username: string
) {
	const ctx = callContext(event);
	try {
		const { data } = await callService<AuthAccount>(ctx, path, {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify(body),
			timeoutMs: 8_000
		});
		writeSession(event.cookies, { ...data, role: 'user' });
		return null;
	} catch (err) {
		if (!(err instanceof ServiceError)) {
			return fail(503, { code: 'UNAVAILABLE', username } satisfies AuthFailure);
		}
		// 登录失败过多：取一道人机验证题交给页面求解，求解后带上答案重新提交
		if (err.code === 'CHALLENGE_REQUIRED') {
			try {
				const { data } = await callService<AuthChallenge>(ctx, '/api/v1/auth/challenge', { timeoutMs: 4_000 });
				return fail(428, { code: err.code, username, challenge: data } satisfies AuthFailure);
			} catch {
				return fail(503, { code: 'UNAVAILABLE', username } satisfies AuthFailure);
			}
		}
		return fail(err.status >= 400 && err.status < 600 ? err.status : 500, { code: err.code, username } satisfies AuthFailure);
	}
}

export function formText(form: FormData, name: string, max: number): string {
	const value = form.get(name);
	return typeof value === 'string' ? value.slice(0, max) : '';
}
