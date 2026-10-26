<script lang="ts">
	import { enhance } from '$app/forms';
	import { page } from '$app/state';
	import type { AuthChallenge } from '#lib/api/types.ts';
	import AuthShell from '#lib/auth/AuthShell.svelte';
	import PasswordField from '#lib/auth/PasswordField.svelte';
	import { solveChallenge } from '#lib/auth/pow.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { mergeLocalData } from '#lib/local/merge.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';

	let { data, form } = $props();
	const i18n = getI18n();

	let submitting = $state(false);
	let verifying = $state(false);
	let showForgot = $state(false);
	let formEl = $state<HTMLFormElement>();
	let nonce = $state('');
	let solution = $state('');

	const failure = $derived(form as { code?: string; username?: string; field?: string; challenge?: AuthChallenge } | null);
	const message = $derived.by(() => {
		if (!failure?.code || verifying) {
			return '';
		}
		if (failure.code === 'NEED_LOGIN') {
			return i18n.t('auth.wrongCredentials');
		}
		if (failure.code === 'FORBIDDEN') {
			return i18n.t('error.BANNED');
		}
		return i18n.error(failure.code);
	});

	// 登录失败过多（428）：在浏览器里求解人机验证题，带上答案自动重新提交。
	// 自动提交最多连续两次（用户自己点登录时清零），防止题目反复失效时无限循环。
	let autoTries = 0;
	$effect(() => {
		const challenge = failure?.challenge;
		if (!challenge || !formEl || autoTries >= 2) {
			return;
		}
		const controller = new AbortController();
		verifying = true;
		solveChallenge(challenge.nonce, challenge.difficulty, controller.signal)
			.then((answer) => {
				nonce = challenge.nonce;
				solution = answer;
				verifying = false;
				queueMicrotask(() => formEl?.requestSubmit());
			})
			.catch(() => {
				verifying = false;
			});
		return () => controller.abort();
	});

	const registerHref = $derived(i18n.path(`/register?next=${encodeURIComponent(data.next)}`));
</script>

<svelte:head>
	<title>{i18n.t('auth.login')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<AuthShell title={i18n.t('auth.loginTitle')} subtitle={i18n.t('auth.loginSubtitle')}>
	<form
		bind:this={formEl}
		method="post"
		class="flex flex-col gap-4"
		novalidate
		use:enhance={({ submitter }) => {
			submitting = true;
			autoTries = submitter ? 0 : autoTries + 1;
			return async ({ result, update }) => {
				submitting = false;
				if (result.type === 'redirect') {
					await mergeLocalData();
					await session.refresh(true);
				} else {
					nonce = '';
					solution = '';
				}
				await update({ reset: false });
			};
		}}
	>
		<input type="hidden" name="next" value={data.next} />
		<input type="hidden" name="challenge_nonce" value={nonce} />
		<input type="hidden" name="challenge_solution" value={solution} />
		<div>
			<label for="login-username" class="mb-1.5 block text-sm font-medium">{i18n.t('auth.username')}</label>
			<input
				id="login-username"
				name="username"
				class="field"
				required
				maxlength="32"
				autocomplete="username"
				autocapitalize="none"
				spellcheck="false"
				value={failure?.username ?? ''}
				aria-invalid={failure?.field === 'username' ? 'true' : undefined}
				aria-describedby={message ? 'login-error' : undefined}
			/>
		</div>
		<PasswordField
			id="login-password"
			name="password"
			label={i18n.t('auth.password')}
			autocomplete="current-password"
			describedby={message ? 'login-error' : undefined}
			invalid={failure?.code === 'NEED_LOGIN'}
		/>

		{#if verifying}
			<p class="flex items-center gap-2 rounded-xl bg-raised px-4 py-3 text-sm text-fg-muted" role="status">
				<Icon name="shield-check" size={18} class="text-brand-fg" />
				{i18n.t('auth.verifying')}
			</p>
		{:else if message}
			<p id="login-error" class="flex items-start gap-2 rounded-xl bg-danger/10 px-4 py-3 text-sm text-danger" role="alert">
				<Icon name="triangle-alert" size={18} class="mt-px" />
				{message}
			</p>
		{/if}

		<button type="submit" class="btn btn-lg btn-primary mt-2 w-full" disabled={submitting || verifying}>
			{#if submitting || verifying}<Icon name="loader-circle" size={18} class="animate-spin" />{/if}
			{i18n.t('auth.login')}
		</button>

		<button
			type="button"
			class="self-center text-sm text-fg-subtle hover:text-fg"
			aria-expanded={showForgot}
			aria-controls="forgot-help"
			onclick={() => (showForgot = !showForgot)}
		>
			{i18n.t('auth.forgot')}
		</button>
		{#if showForgot}
			<div id="forgot-help" class="rounded-xl border border-line bg-raised/60 p-4 text-[13px] leading-6 text-fg-muted">
				<p class="font-semibold text-fg">{i18n.t('auth.forgotTitle')}</p>
				<p class="mt-1">{i18n.t('auth.forgotBody')}</p>
				<a href={i18n.path('/way-home')} class="mt-2 inline-flex items-center gap-1 font-semibold text-brand-fg hover:underline">
					{i18n.t('auth.contactSupport')}
					<Icon name="chevron-right" size={14} class="rtl:rotate-180" />
				</a>
			</div>
		{/if}
	</form>

	{#snippet footer()}
		{i18n.t('auth.noAccount')}
		<a href={registerHref} class="ms-1 font-semibold text-brand-fg hover:underline">{i18n.t('auth.registerFree')}</a>
	{/snippet}
</AuthShell>
