<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { MeProfile, MeSession } from '#lib/api/types.ts';
	import PasswordField from '#lib/auth/PasswordField.svelte';
	import { relativeTime } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import LanguageMenu from '#lib/i18n/LanguageMenu.svelte';
	import SubHeader from '#lib/me/SubHeader.svelte';
	import ThemeToggle from '#lib/shell/ThemeToggle.svelte';
	import { session } from '#lib/stores/session.svelte.ts';
	import { toasts } from '#lib/stores/toast.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';

	const i18n = getI18n();
	let profile = $state<MeProfile | null>(null);
	let sessions = $state<MeSession[]>([]);
	let nickname = $state('');
	let bio = $state('');
	let saving = $state(false);
	let oldPassword = $state('');
	let newPassword = $state('');
	let changing = $state(false);
	let passwordError = $state('');

	const code = (err: unknown) => (err && typeof err === 'object' && 'code' in err ? String((err as { code: unknown }).code) : 'NETWORK');

	$effect(() => {
		if (session.status === 'guest') {
			void goto(i18n.path('/login?next=%2Fme%2Fsettings'), { replace: true });
			return;
		}
		if (session.status !== 'user') {
			return;
		}
		void (async () => {
			const { api } = await import('#lib/api/client.ts');
			const [p, s] = await Promise.all([
				api.get<MeProfile>('/api/v1/me/profile'),
				api.get<{ sessions: MeSession[] }>('/api/v1/me/sessions').catch(() => ({ sessions: [] }))
			]);
			profile = p;
			nickname = p.nickname;
			bio = p.bio;
			sessions = s.sessions;
		})().catch(() => toasts.show(i18n.error('NETWORK'), { tone: 'error' }));
	});

	async function saveProfile(event: SubmitEvent) {
		event.preventDefault();
		saving = true;
		try {
			const { api } = await import('#lib/api/client.ts');
			profile = await api.patch<MeProfile>('/api/v1/me/profile', { nickname: nickname.trim(), bio: bio.trim() });
			await session.refresh(true);
			toasts.show(i18n.t('settings.saved'), { tone: 'success' });
		} catch (err) {
			toasts.show(i18n.error(code(err)), { tone: 'error' });
		} finally {
			saving = false;
		}
	}

	async function changePassword(event: SubmitEvent) {
		event.preventDefault();
		passwordError = '';
		if (newPassword.length < 8) {
			passwordError = i18n.t('auth.passwordWeak');
			return;
		}
		changing = true;
		try {
			const { api } = await import('#lib/api/client.ts');
			await api.post('/api/v1/me/password', { old_password: oldPassword, new_password: newPassword });
			// 改密成功后全部设备都已退出（BFF 同时清掉了本机 Cookie）
			await session.refresh(true);
			toasts.show(i18n.t('settings.passwordChanged'), { tone: 'success', duration: 5000 });
			await goto(i18n.path('/login?next=%2Fme'), { replace: true });
		} catch (err) {
			const c = code(err);
			passwordError = c === 'NEED_LOGIN' || c === 'FORBIDDEN' ? i18n.t('settings.wrongPassword') : c === 'BAD_REQUEST' ? i18n.t('auth.passwordWeak') : i18n.error(c);
		} finally {
			changing = false;
		}
	}

	async function revoke(item: MeSession) {
		try {
			const { api } = await import('#lib/api/client.ts');
			await api.del(`/api/v1/me/sessions/${encodeURIComponent(item.id)}`);
			sessions = sessions.filter((s) => s.id !== item.id);
			toasts.show(i18n.t('settings.revoked'), { tone: 'success' });
		} catch (err) {
			toasts.show(i18n.error(code(err)), { tone: 'error' });
		}
	}

	const PLATFORM = { web: 'settings.platformWeb', ios: 'settings.platformIos', android: 'settings.platformAndroid' } as const;
</script>

<svelte:head>
	<title>{i18n.t('me.settings')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="page-x max-w-3xl! pt-4 lg:pt-8">
	<SubHeader title={i18n.t('me.settings')} />

	<div class="flex flex-col gap-5">
		<form class="rounded-[1.5rem] border border-line bg-surface p-5" onsubmit={saveProfile}>
			<h2 class="mb-4 text-base font-bold">{i18n.t('settings.profile')}</h2>
			{#if profile}
				<div class="flex flex-col gap-4">
					<div>
						<label for="set-nickname" class="mb-1.5 block text-sm font-medium">{i18n.t('auth.nickname')}</label>
						<input id="set-nickname" class="field" bind:value={nickname} maxlength="40" autocomplete="nickname" />
						{#if profile.nickname_status === 'pending'}<p class="mt-1.5 text-xs text-warn">{i18n.t('settings.reviewing')}</p>{/if}
					</div>
					<div>
						<label for="set-bio" class="mb-1.5 block text-sm font-medium">{i18n.t('settings.bio')}</label>
						<textarea id="set-bio" class="field" bind:value={bio} maxlength="80" rows="3" aria-describedby="set-bio-count"></textarea>
						<p id="set-bio-count" class="mt-1 text-end text-xs text-fg-subtle tabular">{bio.length}/80</p>
					</div>
					<button type="submit" class="btn btn-primary self-start" disabled={saving}>
						{#if saving}<Icon name="loader-circle" size={16} class="animate-spin" />{/if}{i18n.t('common.save')}
					</button>
				</div>
			{:else}
				<div class="flex flex-col gap-3" aria-hidden="true"><span class="skeleton h-12"></span><span class="skeleton h-24"></span></div>
			{/if}
		</form>

		<form class="rounded-[1.5rem] border border-line bg-surface p-5" onsubmit={changePassword}>
			<h2 class="mb-1 text-base font-bold">{i18n.t('settings.password')}</h2>
			<p class="mb-4 text-[13px] text-fg-subtle">{i18n.t('settings.passwordNote')}</p>
			<div class="flex flex-col gap-4">
				<PasswordField id="set-old" name="old_password" label={i18n.t('settings.oldPassword')} autocomplete="current-password" bind:value={oldPassword} />
				<PasswordField
					id="set-new"
					name="new_password"
					label={i18n.t('settings.newPassword')}
					autocomplete="new-password"
					describedby="set-new-hint"
					invalid={!!passwordError}
					bind:value={newPassword}
				/>
				<p id="set-new-hint" class="-mt-2 text-xs text-fg-subtle">{i18n.t('auth.passwordHint')}</p>
				{#if passwordError}<p class="text-sm text-danger" role="alert">{passwordError}</p>{/if}
				<button type="submit" class="btn btn-ghost self-start" disabled={changing || !oldPassword || !newPassword}>
					{#if changing}<Icon name="loader-circle" size={16} class="animate-spin" />{/if}{i18n.t('settings.changePassword')}
				</button>
			</div>
		</form>

		{#if sessions.length > 0}
			<section class="rounded-[1.5rem] border border-line bg-surface p-5" aria-labelledby="sessions-title">
				<h2 id="sessions-title" class="mb-4 text-base font-bold">{i18n.t('settings.sessions')}</h2>
				<ul class="divide-y divide-line">
					{#each sessions as item (item.id)}
						<li class="flex items-center gap-3 py-3">
							<span class="grid size-10 place-items-center rounded-xl bg-raised text-fg-muted">
								<Icon name={item.platform === 'web' ? 'laptop' : 'smartphone'} size={19} />
							</span>
							<span class="min-w-0 flex-1">
								<span class="block text-sm font-semibold">
									{i18n.t(PLATFORM[item.platform])}
									{#if item.current}<span class="ms-1 rounded bg-brand-soft px-1.5 text-[11px] text-brand-fg">{i18n.t('settings.current')}</span>{/if}
								</span>
								<span class="block text-xs text-fg-subtle">
									{i18n.t('settings.lastSeen', { time: relativeTime(item.last_seen_at, i18n.locale) })}{item.ip_prefix ? ` · ${item.ip_prefix}` : ''}
								</span>
							</span>
							{#if !item.current}
								<button type="button" class="btn btn-sm btn-ghost" onclick={() => revoke(item)}>{i18n.t('settings.revoke')}</button>
							{/if}
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		<section class="rounded-[1.5rem] border border-line bg-surface p-5" aria-labelledby="look-title">
			<h2 id="look-title" class="mb-4 text-base font-bold">{i18n.t('me.preferences')}</h2>
			<div class="flex flex-col gap-4">
				<!-- 后台关掉「允许切换深浅色」时整行都不出现，否则只剩一个空标签 -->
				{#if page.data.site.theme.allowSchemeToggle}
					<div class="flex flex-wrap items-center justify-between gap-3">
						<span class="text-sm text-fg-muted">{i18n.t('theme.label')}</span>
						<ThemeToggle variant="segmented" />
					</div>
				{/if}
				<div class="flex flex-wrap items-center justify-between gap-3">
					<span class="text-sm text-fg-muted">{i18n.t('lang.label')}</span>
					<LanguageMenu placement="up" />
				</div>
			</div>
		</section>
	</div>
</div>
