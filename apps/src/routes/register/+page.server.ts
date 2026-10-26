import { fail, redirect } from '@sveltejs/kit';
import { localizePath } from '#lib/i18n/locales.ts';
import { authenticate, formText, type AuthFailure } from '#lib/server/auth-actions.ts';
import { safeNext } from '#lib/server/session.ts';
import type { Actions, PageServerLoad } from './$types';

const USERNAME = /^[A-Za-z0-9_]{3,32}$/;

export const load: PageServerLoad = ({ url }) => ({ next: safeNext(url.searchParams.get('next'), '/me') });

export const actions: Actions = {
	default: async (event) => {
		const form = await event.request.formData();
		const username = formText(form, 'username', 32).trim();
		const password = formText(form, 'password', 128);
		const nickname = formText(form, 'nickname', 40).trim();
		const next = safeNext(formText(form, 'next', 500), '/me');
		// 必须勾选「我已知晓忘记密码无法找回」（adr/0001），后端同样校验 ack_no_recovery
		if (form.get('ack') !== 'on') {
			return fail(400, { code: 'ACK_REQUIRED', username, field: 'ack' } satisfies AuthFailure);
		}
		if (!USERNAME.test(username)) {
			return fail(400, { code: 'USERNAME_INVALID', username, field: 'username' } satisfies AuthFailure);
		}
		if (password.length < 8 || /^\d+$/.test(password) || password.toLowerCase() === username.toLowerCase()) {
			return fail(400, { code: 'PASSWORD_WEAK', username, field: 'password' } satisfies AuthFailure);
		}
		const failure = await authenticate(
			event,
			'/api/v1/auth/register',
			{ username, password, ack_no_recovery: true, ...(nickname ? { nickname } : {}) },
			username
		);
		if (failure) {
			return failure;
		}
		redirect(303, localizePath(next, event.locals.locale));
	}
};
