import { redirect } from '@sveltejs/kit';
import { localizePath } from '#lib/i18n/locales.ts';
import { callService } from '#lib/server/api.ts';
import { callContext } from '#lib/server/context.ts';
import { clearSession, SESSION_COOKIES } from '#lib/server/session.ts';
import type { Actions, PageServerLoad } from './$types';

// 退出只接受 POST（表单动作自带来源校验），GET 打开这个地址只回到「我的」，防止被外站链接诱导退出
export const load: PageServerLoad = ({ locals }) => {
	redirect(303, localizePath('/me', locals.locale));
};

export const actions: Actions = {
	default: async (event) => {
		const refresh = event.cookies.get(SESSION_COOKIES.refresh);
		if (refresh) {
			// 吊销服务端会话族；失败也照常清掉本机 Cookie（令牌 30 天后自然过期）
			await callService(callContext(event), '/api/v1/auth/logout', {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ refresh_token: refresh }),
				timeoutMs: 4_000
			}).catch(() => undefined);
		}
		clearSession(event.cookies);
		redirect(303, localizePath('/', event.locals.locale));
	}
};
