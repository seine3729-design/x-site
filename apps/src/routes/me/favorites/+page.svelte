<script lang="ts">
	import { page } from '$app/state';
	import type { DramaCard as Card, FollowItem } from '#lib/api/types.ts';
	import DramaCardView from '#lib/drama/DramaCard.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { localFollows } from '#lib/local/store.ts';
	import SubHeader from '#lib/me/SubHeader.svelte';
	import { session } from '#lib/stores/session.svelte.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import Skeletons from '#lib/ui/Skeletons.svelte';

	const i18n = getI18n();
	let items = $state<(Card & { watched_episode?: number })[] | null>(null);
	let fromAccount = $state(false);

	// 登录用户读账号的追剧列表（GET /api/v1/me/follows）；未登录或请求失败时退回本机记录
	$effect(() => {
		if (session.status === 'unknown') {
			return;
		}
		const loggedIn = session.loggedIn;
		(async () => {
			if (loggedIn) {
				try {
					const { api } = await import('#lib/api/client.ts');
					const remote = await api.get<{ list: FollowItem[] }>('/api/v1/me/follows');
					fromAccount = true;
					items = remote.list;
					return;
				} catch {
					// 失败时退回本机记录
				}
			}
			const local = await localFollows.list();
			items = local.map((f) => ({ ...f, slug: '', cover_landscape: '', publish_at: null }));
		})();
	});

	/** 卡片标题下的一行：优先提示还有几集没看，这是追剧列表最有用的信息 */
	function note(item: Card & { watched_episode?: number }): string | undefined {
		const watched = item.watched_episode;
		if (watched === undefined) {
			return undefined;
		}
		if (watched <= 0) {
			return i18n.t('follows.notStarted');
		}
		const left = item.latest_episode - watched;
		return left > 0 ? i18n.t('follows.unwatched', { n: left }) : i18n.t('follows.caughtUp', { n: watched });
	}
</script>

<svelte:head>
	<title>{i18n.t('me.follows')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="page-x pt-4 lg:pt-8">
	<SubHeader title={i18n.t('me.follows')} />
	{#if items !== null && !fromAccount}
		<p class="mb-5 flex items-center gap-2 rounded-2xl bg-raised px-4 py-3 text-[13px] text-fg-muted">
			<Icon name="info" size={16} class="shrink-0 text-brand-fg" />
			{i18n.t(session.loggedIn ? 'follows.syncNote' : 'follows.localNote')}
		</p>
	{/if}
	{#if items === null}
		<Skeletons count={12} />
	{:else if items.length === 0}
		<EmptyState icon="bookmark" title={i18n.t('follows.empty')} description={i18n.t('follows.emptyDesc')}>
			<a href={i18n.path('/explore')} class="btn btn-primary">{i18n.t('home.goExplore')}</a>
		</EmptyState>
	{:else}
		<ul class="grid grid-cols-3 gap-x-3 gap-y-5 sm:grid-cols-4 lg:grid-cols-6 lg:gap-x-4">
			{#each items as item, index (item.id)}
				<li><DramaCardView card={item} priority={index < 6} note={note(item)} /></li>
			{/each}
		</ul>
	{/if}
</div>
