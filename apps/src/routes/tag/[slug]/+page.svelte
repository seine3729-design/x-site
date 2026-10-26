<script lang="ts">
	import { page } from '$app/state';
	import InfiniteGrid from '#lib/drama/InfiniteGrid.svelte';
	import SortTabs from '#lib/explore/SortTabs.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';

	let { data } = $props();
	const i18n = getI18n();
	const tag = $derived(data.result.tag);
	const base = $derived(i18n.path(`/tag/${encodeURIComponent(tag.slug)}`));
	const sortOptions = $derived(
		(['latest', 'hot', 'recommend'] as const).map((value) => ({
			value,
			key: `sort.${value}` as const,
			href: base + (value === 'latest' ? '' : `?sort=${value}`)
		}))
	);
	const nextPath = (cursor: string) =>
		`/api/v1/tags/${encodeURIComponent(tag.slug)}?sort=${data.sort}&limit=24&cursor=${encodeURIComponent(cursor)}`;
</script>

<svelte:head>
	<title>{i18n.t('seo.tagTitle', { name: tag.name, site: page.data.site.name })}</title>
	<meta name="description" content={i18n.t('seo.tagDesc', { name: tag.name })} />
	{#if tag.is_sensitive}
		<meta name="robots" content="noindex" />
	{/if}
</svelte:head>

<div class="page-x pt-4 lg:pt-8">
	<div class="mb-5 flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-extrabold tracking-tight lg:text-3xl"><span class="text-brand-fg">#</span>{tag.name}</h1>
			<p class="mt-1 text-[13px] text-fg-muted">{i18n.t('explore.total', { n: data.result.total })}</p>
		</div>
		<SortTabs options={sortOptions} current={data.sort} label={i18n.t('explore.sort')} />
	</div>
	{#if data.result.list.length === 0}
		<EmptyState icon="tag" title={i18n.t('explore.emptyTitle')} description={i18n.t('channel.emptyDesc')}>
			<a href={i18n.path('/explore')} class="btn btn-ghost">{i18n.t('home.goExplore')}</a>
		</EmptyState>
	{:else}
		<InfiniteGrid initial={data.result} {nextPath} />
	{/if}
</div>
