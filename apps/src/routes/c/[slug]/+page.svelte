<script lang="ts">
	import { page } from '$app/state';
	import DramaGrid from '#lib/drama/DramaGrid.svelte';
	import SortTabs from '#lib/explore/SortTabs.svelte';
	import BlockRenderer from '#lib/home/BlockRenderer.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Pager from '#lib/ui/Pager.svelte';
	import { searchString } from '#lib/url.ts';

	let { data } = $props();
	const i18n = getI18n();
	const channel = $derived(data.list.channel);
	const base = $derived(i18n.path(`/c/${encodeURIComponent(channel.slug)}`));
	const pages = $derived(Math.min(100, Math.ceil(data.list.total / data.pageSize)));

	const search = (sort: string, pageNumber: number) =>
		searchString({ sort: sort === 'latest' ? '' : sort, page: pageNumber > 1 ? pageNumber : '' });
	const sortOptions = $derived(
		(['latest', 'hot', 'recommend'] as const).map((value) => ({ value, key: `sort.${value}` as const, href: base + search(value, 1) }))
	);
</script>

<svelte:head>
	<title>{i18n.t('seo.channelTitle', { name: channel.name, site: page.data.site.name })}</title>
	<meta name="description" content={i18n.t('seo.channelDesc', { name: channel.name })} />
	<link rel="canonical" href={page.url.origin + base + search(data.sort, data.page)} />
</svelte:head>

<div class="page-x pt-4 lg:pt-8">
	{#if data.blocks.length > 0}
		<div class="mb-10 flex flex-col gap-10">
			{#each data.blocks as block, index (block.id)}
				<BlockRenderer {block} {index} />
			{/each}
		</div>
	{/if}

	<div class="mb-5 flex flex-wrap items-end justify-between gap-3">
		<div>
			<p class="text-[13px] text-fg-subtle">{channel.taxonomy_name}</p>
			<h1 class="text-2xl font-extrabold tracking-tight lg:text-3xl">{channel.name}</h1>
			<p class="mt-1 text-[13px] text-fg-muted">{i18n.t('explore.total', { n: data.list.total })}</p>
		</div>
		<SortTabs options={sortOptions} current={data.sort} label={i18n.t('explore.sort')} />
	</div>

	{#if data.list.list.length === 0}
		<EmptyState title={i18n.t('explore.emptyTitle')} description={i18n.t('channel.emptyDesc')}>
			<a href={i18n.path('/explore')} class="btn btn-ghost">{i18n.t('home.goExplore')}</a>
		</EmptyState>
	{:else}
		<DramaGrid cards={data.list.list} priorityCount={6} />
		<Pager page={data.page} {pages} href={(p) => base + search(data.sort, p)} />
	{/if}
</div>
