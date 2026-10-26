import { fail, redirect } from '@sveltejs/kit';
import { localizePath } from '#lib/i18n/locales.ts';
import { authenticate, formText, type AuthFailure } from '#lib/server/auth-actions.ts';
import { safeNext } from '#lib/server/session.ts';
import type { Actions, PageServerLoad } from './$types';

export const load: PageServerLoad = ({ url }) => ({ next: safeNext(url.searchParams.get('next'), '/me') });

export const actions: Actions = {
	default: async (event) => {
		const form = await event.request.formData();
		const username = formText(form, 'username', 32).trim();
		const password = formText(form, 'password', 128);
		const next = safeNext(formText(form, 'next', 500), '/me');
		if (!username || !password) {
			return fail(400, { code: 'BAD_REQUEST', username, field: username ? 'password' : 'username' } satisfies AuthFailure);
		}
		const nonce = formText(form, 'challenge_nonce', 128);
		const solution = formText(form, 'challenge_solution', 64);
		const failure = await authenticate(
			event,
			'/api/v1/auth/login',
			{ username, password, ...(nonce && solution ? { challenge: { nonce, solution } } : {}) },
			username
		);
		if (failure) {
			return failure;
		}
		redirect(303, localizePath(next, event.locals.locale));
	}
};
