<script lang="ts">
	import { enhance } from '$app/forms';
	import { page } from '$app/state';
	import AuthShell from '#lib/auth/AuthShell.svelte';
	import PasswordField from '#lib/auth/PasswordField.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { mergeLocalData } from '#lib/local/merge.ts';
	import { featureOn, getSite } from '#lib/site.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';

	let { data, form } = $props();
	const i18n = getI18n();
	const site = $derived(getSite());
	const open = $derived(featureOn(site.features, 'register'));

	let submitting = $state(false);
	let password = $state('');
	let confirm = $state('');
	let ack = $state(false);

	const USERNAME_PATTERN = '[A-Za-z0-9_]{3,32}';
	const failure = $derived(form as { code?: string; username?: string; field?: string } | null);
	const LOCAL_CODES: Record<string, 'auth.ackRequired' | 'auth.usernameInvalid' | 'auth.passwordWeak' | 'auth.usernameTaken'> = {
		ACK_REQUIRED: 'auth.ackRequired',
		USERNAME_INVALID: 'auth.usernameInvalid',
		PASSWORD_WEAK: 'auth.passwordWeak',
		CONFLICT: 'auth.usernameTaken'
	};
	const message = $derived(
		failure?.code ? (LOCAL_CODES[failure.code] ? i18n.t(LOCAL_CODES[failure.code]!) : failure.code === 'BAD_REQUEST' ? i18n.t('auth.registerInvalid') : i18n.error(failure.code)) : ''
	);
	const mismatch = $derived(confirm.length > 0 && confirm !== password);
	const loginHref = $derived(i18n.path(`/login?next=${encodeURIComponent(data.next)}`));
</script>

<svelte:head>
	<title>{i18n.t('auth.register')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

{#if !open}
	<div class="page-x">
		<EmptyState icon="lock" title={i18n.t('auth.registerClosed')} description={i18n.t('auth.registerClosedDesc')}>
			<a href={loginHref} class="btn btn-primary">{i18n.t('auth.login')}</a>
		</EmptyState>
	</div>
{:else}
	<AuthShell title={i18n.t('auth.registerTitle')} subtitle={i18n.t('auth.registerSubtitle')}>
		<form
			method="post"
			class="flex flex-col gap-4"
			use:enhance={({ cancel }) => {
				if (mismatch) {
					cancel();
					return;
				}
				submitting = true;
				return async ({ result, update }) => {
					submitting = false;
					if (result.type === 'redirect') {
						// 表单动作只在注册成功时跳转（注册墙漏斗的最后一步，W4-1）；之后是站内跳转，页面不卸载
						void import('#lib/analytics/runtime.ts').then((analytics) => analytics.trackRegistered());
						await mergeLocalData();
						await session.refresh(true);
					}
					await update({ reset: false });
				};
			}}
		>
			<input type="hidden" name="next" value={data.next} />
			<div>
				<label for="reg-username" class="mb-1.5 block text-sm font-medium">{i18n.t('auth.username')}</label>
				<input
					id="reg-username"
					name="username"
					class="field"
					required
					minlength="3"
					maxlength="32"
					pattern={USERNAME_PATTERN}
					autocomplete="username"
					autocapitalize="none"
					spellcheck="false"
					value={failure?.username ?? ''}
					aria-describedby="reg-username-hint"
					aria-invalid={failure?.field === 'username' || failure?.code === 'CONFLICT' ? 'true' : undefined}
				/>
				<p id="reg-username-hint" class="mt-1.5 text-xs text-fg-subtle">{i18n.t('auth.usernameHint')}</p>
			</div>
			<div>
				<label for="reg-nickname" class="mb-1.5 block text-sm font-medium">
					{i18n.t('auth.nickname')} <span class="font-normal text-fg-subtle">{i18n.t('auth.optional')}</span>
				</label>
				<input id="reg-nickname" name="nickname" class="field" maxlength="40" autocomplete="nickname" />
			</div>
			<div>
				<PasswordField
					id="reg-password"
					name="password"
					label={i18n.t('auth.password')}
					autocomplete="new-password"
					describedby="reg-password-hint"
					invalid={failure?.field === 'password'}
					bind:value={password}
				/>
				<p id="reg-password-hint" class="mt-1.5 text-xs text-fg-subtle">{i18n.t('auth.passwordHint')}</p>
			</div>
			<div>
				<PasswordField
					id="reg-confirm"
					name="password_confirm"
					label={i18n.t('auth.passwordConfirm')}
					autocomplete="new-password"
					describedby={mismatch ? 'reg-confirm-error' : undefined}
					invalid={mismatch}
					bind:value={confirm}
				/>
				{#if mismatch}<p id="reg-confirm-error" class="mt-1.5 text-xs text-danger">{i18n.t('auth.passwordMismatch')}</p>{/if}
			</div>

			<div class="rounded-2xl border border-warn/40 bg-warn/10 p-4">
				<p class="mt-1.5 text-[13px] leading-6 text-fg-muted">{i18n.t('auth.noRecoveryBody')}</p>
				<label class="mt-3 flex cursor-pointer items-start gap-2.5 text-sm font-medium">
					<input
						type="checkbox"
						name="ack"
						bind:checked={ack}
						required
						class="mt-0.5 size-5 shrink-0 accent-[var(--brand)]"
						aria-invalid={failure?.field === 'ack' ? 'true' : undefined}
					/>
					{i18n.t('auth.ack')}
				</label>
			</div>

			{#if message}
				<p class="flex items-start gap-2 rounded-xl bg-danger/10 px-4 py-3 text-sm text-danger" role="alert">
					<Icon name="triangle-alert" size={18} class="mt-px" />{message}
				</p>
			{/if}

			<!-- 未勾选时由复选框的 required 拦住（无 JS 也生效）；按钮本身不在 SSR 时禁用 -->
			<button type="submit" class="btn btn-lg btn-primary mt-1 w-full" disabled={submitting || mismatch}>
				{#if submitting}<Icon name="loader-circle" size={18} class="animate-spin" />{/if}
				{i18n.t('auth.registerSubmit')}
			</button>
		</form>

		{#snippet footer()}
			{i18n.t('auth.haveAccount')}
			<a href={loginHref} class="ms-1 font-semibold text-brand-fg hover:underline">{i18n.t('auth.login')}</a>
		{/snippet}
	</AuthShell>
{/if}
