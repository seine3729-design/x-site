<script lang="ts">
	import { page } from '$app/state';
	import InfiniteGrid from '#lib/drama/InfiniteGrid.svelte';
	import FilterPanel from '#lib/explore/FilterPanel.svelte';
	import { exploreParams, exploreSearch, SORTS, toggleTerm } from '#lib/explore/query.ts';
	import SortTabs from '#lib/explore/SortTabs.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import Modal from '#lib/ui/Modal.svelte';
	import { searchString } from '#lib/url.ts';

	let { data } = $props();
	const i18n = getI18n();
	const query = $derived(data.query);
	const taxonomies = $derived(data.facets.taxonomies);
	let filtersOpen = $state(false);

	const href = (search: string) => i18n.path('/explore') + search;
	const sortOptions = $derived(
		SORTS.map((value) => ({ value, key: `sort.${value}` as const, href: href(exploreSearch({ ...query, sort: value })) }))
	);

	// 已选条件：可以单个去掉或全部清空
	const chosen = $derived.by(() => {
		const items: { label: string; href: string }[] = [];
		for (const key of query.terms) {
			const [tax, slug] = key.split(':');
			const taxonomy = taxonomies.find((t) => t.slug === tax);
			const term = taxonomy?.terms.find((t) => t.slug === slug);
			if (taxonomy && term) {
				items.push({ label: term.name, href: href(exploreSearch(toggleTerm(query, tax!, slug!, true))) });
			}
		}
		if (query.finished) {
			items.push({
				label: i18n.t(query.finished === 'true' ? 'explore.finished' : 'explore.ongoing'),
				href: href(exploreSearch({ ...query, finished: '' }))
			});
		}
		if (query.window !== 'all') {
			items.push({ label: i18n.t(`explore.window${query.window}` as 'explore.window7d'), href: href(exploreSearch({ ...query, window: 'all' })) });
		}
		if (query.tag) {
			items.push({ label: `#${query.tag}`, href: href(exploreSearch({ ...query, tag: '' })) });
		}
		return items;
	});

	const nextPath = (cursor: string) => `/api/v1/explore${searchString({ ...exploreParams(query), cursor, limit: 24 })}`;
</script>

<svelte:head>
	<title>{i18n.t('explore.title')} - {page.data.site.name}</title>
	<meta name="description" content={i18n.t('explore.description')} />
	{#if chosen.length > 0}
		<link rel="canonical" href={page.url.origin + i18n.path('/explore')} />
	{/if}
</svelte:head>

<div class="page-x pt-4 lg:pt-8">
	<div class="mb-4 flex flex-wrap items-center justify-between gap-3 lg:mb-6">
		<h1 class="text-2xl font-extrabold tracking-tight lg:text-3xl">{i18n.t('explore.title')}</h1>
		<div class="flex items-center gap-2">
			<SortTabs options={sortOptions} current={query.sort} label={i18n.t('explore.sort')} />
			<button
				type="button"
				class="btn btn-sm btn-ghost lg:hidden"
				aria-haspopup="dialog"
				onclick={() => (filtersOpen = true)}
			>
				<Icon name="sliders-horizontal" size={16} />
				{i18n.t('explore.filters')}
				{#if chosen.length > 0}<span class="rounded-full bg-brand px-1.5 text-[11px] text-on-brand">{chosen.length}</span>{/if}
			</button>
		</div>
	</div>

	<div class="mb-6 hidden rounded-panel border border-line bg-surface p-5 lg:block">
		<FilterPanel {query} {taxonomies} basePath="/explore" />
	</div>

	{#if chosen.length > 0}
		<div class="mb-5 flex flex-wrap items-center gap-2">
			<span class="text-[13px] text-fg-subtle">{i18n.t('explore.total', { n: data.result.total })}</span>
			{#each chosen as item (item.href)}
				<a href={item.href} class="chip gap-1 bg-brand-soft text-brand-fg" data-sveltekit-noscroll>
					{item.label}
					<Icon name="x" size={13} />
					<span class="sr-only">{i18n.t('explore.removeFilter')}</span>
				</a>
			{/each}
			<a href={i18n.path('/explore') + exploreSearch({ ...query, terms: [], finished: '', window: 'all', tag: '' })} class="text-[13px] font-semibold text-brand-fg hover:underline" data-sveltekit-noscroll>
				{i18n.t('explore.clear')}
			</a>
		</div>
	{/if}

	{#if data.result.list.length === 0}
		<EmptyState icon="compass" title={i18n.t('explore.emptyTitle')} description={i18n.t('explore.emptyDesc')}>
			<a href={i18n.path('/explore')} class="btn btn-ghost">{i18n.t('explore.clear')}</a>
		</EmptyState>
	{:else}
		<InfiniteGrid initial={data.result} {nextPath} />
	{/if}
</div>

<Modal open={filtersOpen} label={i18n.t('explore.filters')} onclose={() => (filtersOpen = false)} variant="sheet">
	<div class="mb-5 flex items-center justify-between">
		<h2 class="text-lg font-bold">{i18n.t('explore.filters')}</h2>
		<button type="button" class="icon-btn -me-2" aria-label={i18n.t('common.close')} onclick={() => (filtersOpen = false)}>
			<Icon name="x" size={20} />
		</button>
	</div>
	<FilterPanel {query} {taxonomies} basePath="/explore" />
	<button type="button" class="btn btn-primary mt-6 w-full" onclick={() => (filtersOpen = false)}>
		{i18n.t('explore.showResults', { n: data.result.total })}
	</button>
</Modal>
