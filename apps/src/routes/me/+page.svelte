<script lang="ts">
	import { enhance } from '$app/forms';
	import { page } from '$app/state';
	import type { VipStatus } from '#lib/api/types.ts';
	import DramaCard from '#lib/drama/DramaCard.svelte';
	import { playPath } from '#lib/drama/labels.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import LanguageMenu from '#lib/i18n/LanguageMenu.svelte';
	import { loadHistory, progressOf, type HistoryRow } from '#lib/me/history.ts';
	import ThemeToggle from '#lib/shell/ThemeToggle.svelte';
	import { session } from '#lib/stores/session.svelte.ts';
	import Avatar from '#lib/ui/Avatar.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import Rail from '#lib/ui/Rail.svelte';
	import SectionHeader from '#lib/ui/SectionHeader.svelte';

	const i18n = getI18n();
	let history = $state<HistoryRow[] | null>(null);
	let vipStatus = $state<VipStatus | null>(null);
	let vipPending = $state(false);
	let vipFailed = $state(false);

	// 个人数据只在浏览器里取（页面不进任何缓存）：登录态确定后再读历史
	$effect(() => {
		if (session.status === 'unknown') {
			return;
		}
		const loggedIn = session.loggedIn;
		loadHistory(loggedIn)
			.then((rows) => (history = rows.slice(0, 12)))
			.catch(() => (history = []));
	});

	// 会员状态同样是个人数据，不能混入 SSR 或公共页面缓存；读取失败时不把用户错误标成非会员。
	$effect(() => {
		const isUser = session.status === 'user';
		if (!isUser) {
			vipStatus = null;
			vipPending = false;
			vipFailed = false;
			return;
		}
		let cancelled = false;
		vipPending = true;
		vipFailed = false;
		void import('#lib/api/client.ts')
			.then(({ api }) => api.get<VipStatus>('/api/v1/me/vip'))
			.then((status) => {
				if (!cancelled) vipStatus = status;
			})
			.catch(() => {
				if (!cancelled) vipFailed = true;
			})
			.finally(() => {
				if (!cancelled) vipPending = false;
			});
		return () => {
			cancelled = true;
		};
	});

	const unread = $derived(session.me?.unread ?? 0);
	const entries = $derived([
		{ href: '/me/history', icon: 'history', key: 'me.history' },
		{ href: '/me/favorites', icon: 'bookmark', key: 'me.follows' },
		{ href: '/me/messages', icon: 'bell', key: 'me.messages', badge: unread, auth: true },
		{ href: '/me/settings', icon: 'settings', key: 'me.settings', auth: true },
		{ href: '/vip', icon: 'crown', key: 'me.vip' },
		{ href: '/way-home', icon: 'map-pin', key: 'footer.wayHome' }
	] as const);
	const loginHref = $derived(i18n.path('/login?next=%2Fme'));
	const card = (row: HistoryRow) => ({
		id: row.drama_id,
		title: row.title,
		slug: '',
		cover_portrait: row.cover,
		cover_landscape: '',
		badge: '',
		score: null,
		year: null,
		is_finished: false,
		latest_episode: row.latest_episode,
		play_count: 0,
		publish_at: null
	});

	function vipTitle() {
		if (session.status !== 'user') return i18n.t('vip.bannerTitle');
		return vipStatus?.is_vip ? i18n.t('vip.memberActive') : i18n.t('vip.memberInactive');
	}

	function vipDescription() {
		if (session.status !== 'user') return i18n.t('vip.bannerDesc');
		if (vipPending) return i18n.t('vip.memberStatusLoading');
		if (vipFailed) return i18n.t('vip.memberStatusUnavailable');
		if (vipStatus?.is_vip && vipStatus.expire_at) {
			const date = new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium' }).format(new Date(vipStatus.expire_at));
			return i18n.t('vip.memberExpiresAt', { date });
		}
		return i18n.t('vip.bannerDesc');
	}
</script>

<svelte:head>
	<title>{i18n.t('nav.me')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="page-x max-w-5xl! pt-5 lg:pt-10">
	<h1 class="sr-only">{i18n.t('nav.me')}</h1>
	<!-- 资料卡 -->
	<section
		class="relative isolate overflow-hidden rounded-[1.75rem] border border-line bg-surface p-5 lg:p-8"
		aria-label={i18n.t('me.profile')}
	>
		<div
			class="absolute inset-0 -z-10 bg-[radial-gradient(60%_120%_at_0%_0%,rgb(225_29_72/0.22),transparent),radial-gradient(50%_100%_at_100%_100%,rgb(147_51_234/0.16),transparent)]"
			aria-hidden="true"
		></div>
		{#if session.status === 'user' && session.me}
			<div class="flex items-center gap-4">
				<Avatar name={session.me.user.nickname} size={64} class="ring-4 ring-white/10" />
				<div class="min-w-0 flex-1">
					<p class="truncate text-xl font-extrabold">{session.me.user.nickname}</p>
					<p class="mt-0.5 text-xs text-fg-subtle tabular">ID {session.me.user.id}</p>
				</div>
				<a href={i18n.path('/me/settings')} class="btn btn-sm btn-ghost">
					<Icon name="square-pen" size={15} />{i18n.t('me.editProfile')}
				</a>
			</div>
		{:else if session.status === 'guest'}
			<div class="flex flex-col gap-4 sm:flex-row sm:items-center">
				<span class="grid size-16 place-items-center rounded-full bg-raised text-fg-subtle"><Icon name="user" size={30} /></span>
				<div class="min-w-0 flex-1">
					<p class="text-xl font-extrabold">{i18n.t('me.guestTitle')}</p>
					<p class="mt-1 text-sm text-fg-muted">{i18n.t('me.guestDesc')}</p>
				</div>
				<div class="flex gap-2">
					<a href={loginHref} class="btn btn-primary">{i18n.t('auth.login')}</a>
					<a href={i18n.path('/register?next=%2Fme')} class="btn btn-ghost">{i18n.t('auth.register')}</a>
				</div>
			</div>
		{:else}
			<div class="flex items-center gap-4" aria-hidden="true">
				<span class="skeleton size-16 rounded-full"></span>
				<span class="flex flex-1 flex-col gap-2"><span class="skeleton h-5 w-40"></span><span class="skeleton h-3 w-24"></span></span>
			</div>
		{/if}

		<!-- 会员状态是浏览器端个人请求，role=status 仅播报完整状态文本，不移动焦点。 -->
		<a
			href={i18n.path('/vip')}
			class="mt-5 flex items-center gap-3 rounded-2xl bg-gradient-to-r from-[#3a2a0d] via-[#2a1f0c] to-[#1a1408] p-4 text-[#f7e3b0] ring-1 ring-[#e3a93a]/30 ring-inset"
		>
			<span class="grid size-10 shrink-0 place-items-center rounded-xl bg-gradient-to-br from-[#f7d27a] to-[#e3a93a] text-on-vip">
				<Icon name="crown" size={20} />
			</span>
			<span class="min-w-0 flex-1">
				<span class="block text-[15px] font-bold">{vipTitle()}</span>
				<span class="block truncate text-xs text-[#f7e3b0]/70" role="status" aria-atomic="true">{vipDescription()}</span>
			</span>
			<span class="btn btn-sm btn-vip shrink-0">{i18n.t(session.status === 'user' ? 'vip.viewMembership' : 'vip.cta')}</span>
		</a>
	</section>

	<!-- 常用入口 -->
	<nav class="mt-5 grid grid-cols-3 gap-2.5 sm:grid-cols-6" aria-label={i18n.t('me.shortcuts')}>
		{#each entries as entry (entry.href)}
			{@const locked = 'auth' in entry && entry.auth && session.status === 'guest'}
			<a
				href={i18n.path(locked ? `/login?next=${encodeURIComponent(entry.href)}` : entry.href)}
				class="relative flex flex-col items-center gap-2 rounded-panel border border-line bg-surface py-4 text-[13px] font-medium text-fg-muted transition-colors hover:border-line-strong hover:text-fg"
			>
				<Icon name={entry.icon} size={22} class="text-fg" />
				{i18n.t(entry.key)}
				{#if 'badge' in entry && entry.badge > 0}
					<span class="absolute end-3 top-2.5 min-w-5 rounded-full bg-brand px-1.5 text-center text-[11px] leading-5 font-bold text-on-brand">
						{entry.badge > 99 ? '99+' : entry.badge}
					</span>
				{/if}
			</a>
		{/each}
	</nav>

	<!-- 继续观看 -->
	<section class="mt-9" aria-labelledby="continue-title">
		<SectionHeader id="continue-title" title={i18n.t('me.continue')} more={history && history.length > 0 ? '/me/history' : ''} />
		{#if history === null}
			<div class="flex gap-2.5 overflow-hidden" aria-hidden="true">
				{#each { length: 6 }, index (index)}
					<span class="skeleton aspect-cover w-[30%] shrink-0 rounded-card sm:w-[22%] md:w-[18%] lg:w-[15.2%]"></span>
				{/each}
			</div>
		{:else if history.length === 0}
			<p class="rounded-panel border border-dashed border-line px-4 py-8 text-center text-sm text-fg-subtle">{i18n.t('me.continueEmpty')}</p>
		{:else}
			<Rail items={history} key={(row) => row.drama_id} label={i18n.t('me.continue')}>
				{#snippet item(row)}
					<DramaCard
						card={card(row)}
						href={i18n.path(playPath(row.drama_id, row.episode_number))}
						progress={progressOf(row)}
						note={i18n.t('me.watchedTo', { n: row.episode_number })}
					/>
				{/snippet}
			</Rail>
		{/if}
	</section>

	<!-- 偏好设置（不需要登录） -->
	<section class="mt-9 rounded-[1.5rem] border border-line bg-surface p-5" aria-labelledby="prefs-title">
		<h2 id="prefs-title" class="mb-4 text-base font-bold">{i18n.t('me.preferences')}</h2>
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

	{#if session.status === 'user'}
		<form method="post" action={i18n.path('/logout')} class="mt-6" use:enhance={() => async ({ update }) => {
			await update();
			await session.refresh(true);
		}}>
			<button type="submit" class="btn btn-ghost w-full text-danger">
				<Icon name="log-out" size={18} />{i18n.t('auth.logout')}
			</button>
		</form>
	{/if}
</div>
