<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import type { SuggestPage } from '#lib/api/types.ts';
	import InfiniteGrid from '#lib/drama/InfiniteGrid.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { addSearchHistory, clearSearchHistory, readSearchHistory } from '#lib/search/history.ts';
	import HotDramaList from '#lib/search/HotDramaList.svelte';
	import { featureOn, getSite } from '#lib/site.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';

	let { data } = $props();
	const i18n = getI18n();
	const site = $derived(getSite());
	const suggestOn = $derived(featureOn(site.features, 'search_suggest'));

	// 输入框的值跟随地址栏的 q，用户输入时临时覆盖（可写的 $derived）
	let value = $derived(data.q);
	let history = $state<string[]>([]);
	let suggestions = $state<SuggestPage['list']>([]);
	let open = $state(false);
	let active = $state(-1);
	let input = $state<HTMLInputElement>();

	onMount(() => {
		if (!data.q) {
			history = readSearchHistory();
		}
		if (!data.q && matchMedia('(min-width: 1024px)').matches) {
			input?.focus();
		}
	});

	$effect(() => {
		if (data.q) {
			history = addSearchHistory(data.q);
		}
	});

	// 联想：300ms 防抖，新输入取消旧请求（docs/04 B.9）
	$effect(() => {
		const q = value.trim();
		if (!suggestOn || !q || q === data.q) {
			suggestions = [];
			return;
		}
		const controller = new AbortController();
		const timer = setTimeout(async () => {
			try {
				const { api } = await import('#lib/api/client.ts');
				const result = await api.get<SuggestPage>(`/api/v1/search/suggest?q=${encodeURIComponent(q)}&limit=8`, controller.signal);
				suggestions = result.list;
				active = -1;
				open = true;
			} catch {
				suggestions = [];
			}
		}, 300);
		return () => {
			clearTimeout(timer);
			controller.abort();
		};
	});

	function submit(query: string) {
		const q = query.trim();
		open = false;
		if (!q) {
			return;
		}
		void goto(i18n.path(`/search?q=${encodeURIComponent(q)}`), { reset: false });
	}

	function onKeydown(event: KeyboardEvent) {
		if (!open || suggestions.length === 0) {
			return;
		}
		if (event.key === 'ArrowDown') {
			event.preventDefault();
			active = (active + 1) % suggestions.length;
		} else if (event.key === 'ArrowUp') {
			event.preventDefault();
			active = (active - 1 + suggestions.length) % suggestions.length;
		} else if (event.key === 'Enter' && active >= 0) {
			event.preventDefault();
			const pick = suggestions[active];
			open = false;
			if (pick) {
				void goto(i18n.path(`/d/${pick.id}`));
			}
		} else if (event.key === 'Escape') {
			open = false;
		}
	}

	const nextPath = (cursor: string) =>
		`/api/v1/search?q=${encodeURIComponent(data.q)}&limit=24&cursor=${encodeURIComponent(cursor)}`;
</script>

<svelte:head>
	<title>{data.q ? i18n.t('search.resultTitle', { q: data.q }) : i18n.t('search.title')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="page-x pt-4 lg:pt-8">
	<!-- 没输入关键词时页面上没有可见标题（顶部就是个大搜索框），h1 用隐藏的，保证每页都有唯一 h1 -->
	<h1 class="sr-only">{data.q ? i18n.t('search.resultTitle', { q: data.q }) : i18n.t('search.title')}</h1>
	<form
		action={i18n.path('/search')}
		method="get"
		role="search"
		class="relative mx-auto mb-8 max-w-2xl"
		onsubmit={(event) => {
			event.preventDefault();
			submit(value);
		}}
	>
		<label for="search-input" class="sr-only">{i18n.t('search.placeholder')}</label>
		<Icon name="search" size={20} class="pointer-events-none absolute start-5 top-1/2 -translate-y-1/2 text-fg-subtle" />
		<input
			bind:this={input}
			bind:value
			id="search-input"
			name="q"
			type="search"
			enterkeyhint="search"
			autocomplete="off"
			maxlength="50"
			placeholder={i18n.t('search.placeholder')}
			class="h-14 w-full rounded-full border border-line bg-surface ps-13 pe-28 text-base text-fg shadow-sm placeholder:text-fg-subtle focus:border-brand-fg/60 focus:outline-none"
			role="combobox"
			aria-expanded={open && suggestions.length > 0}
			aria-controls="search-suggest"
			aria-autocomplete="list"
			aria-activedescendant={active >= 0 ? `suggest-${active}` : undefined}
			onkeydown={onKeydown}
			onfocus={() => (open = true)}
			onblur={() => setTimeout(() => (open = false), 150)}
		/>
		<button type="submit" class="btn btn-sm btn-primary absolute end-2 top-1/2 -translate-y-1/2">{i18n.t('search.submit')}</button>

		{#if open && suggestions.length > 0}
			<ul
				id="search-suggest"
				role="listbox"
				class="absolute inset-x-0 top-[calc(100%+0.5rem)] z-30 animate-fade-in overflow-hidden rounded-panel border border-line bg-overlay p-1.5 shadow-pop"
			>
				{#each suggestions as item, index (item.id)}
					<li id="suggest-{index}" role="option" aria-selected={index === active}>
						<a
							href={i18n.path(`/d/${item.id}`)}
							class="flex items-center gap-3 rounded-xl px-4 py-2.5 text-[15px] hover:bg-raised {index === active ? 'bg-raised' : ''}"
						>
							<Icon name="search" size={15} class="text-fg-subtle" />
							<span class="truncate">{item.title}</span>
						</a>
					</li>
				{/each}
			</ul>
		{/if}
	</form>

	{#if data.result}
		<div class="mb-5 flex items-baseline justify-between gap-3">
			<h2 class="text-xl font-bold">{i18n.t('search.resultTitle', { q: data.q })}</h2>
			<span class="text-[13px] text-fg-subtle">{i18n.t('explore.total', { n: data.result.total })}</span>
		</div>
		{#if data.result.list.length === 0}
			<EmptyState icon="search" title={i18n.t('search.emptyTitle', { q: data.q })} description={i18n.t('search.emptyDesc')}>
				<a href={i18n.path('/explore')} class="btn btn-ghost">{i18n.t('home.goExplore')}</a>
			</EmptyState>
		{:else}
			<InfiniteGrid initial={data.result} {nextPath} />
		{/if}
	{/if}

	{#if !data.result || data.result.list.length === 0}
		<div class="mx-auto flex max-w-4xl flex-col gap-6">
			{#if history.length > 0}
				<section aria-labelledby="history-title">
					<div class="mb-2.5 flex items-center justify-between px-1">
						<h2 id="history-title" class="text-[15px] font-bold">{i18n.t('search.history')}</h2>
						<button
							type="button"
							class="flex items-center gap-1 text-[13px] text-fg-subtle hover:text-fg"
							onclick={() => {
								clearSearchHistory();
								history = [];
							}}
						>
							<Icon name="trash-2" size={14} />{i18n.t('search.clearHistory')}
						</button>
					</div>
					<ul class="flex flex-wrap gap-2">
						{#each history as word (word)}
							<li><a href={i18n.path(`/search?q=${encodeURIComponent(word)}`)} class="chip h-9 px-4">{word}</a></li>
						{/each}
					</ul>
				</section>
			{/if}

			<!-- 热搜词：名次清单让给下面的「热门影片」，这里用一排 chip（置顶词标「热」） -->
			{#if data.hot.length > 0}
				<section aria-labelledby="hot-title">
					<h2 id="hot-title" class="mb-2.5 flex items-center gap-1.5 px-1 text-[15px] font-bold">
						<Icon name="trending-up" size={17} class="text-brand-fg" />{i18n.t('search.hot')}
					</h2>
					<ul class="flex flex-wrap gap-2">
						{#each data.hot as word (word.word)}
							<li>
								<a href={i18n.path(`/search?q=${encodeURIComponent(word.word)}`)} class="chip h-9 gap-1 px-4">
									{word.word}
									{#if word.pinned}<span class="text-[11px] font-bold text-brand-fg">{i18n.t('search.pinned')}</span>{/if}
								</a>
							</li>
						{/each}
					</ul>
				</section>
			{/if}

			{#if data.hotDramas.length > 0}
				<HotDramaList items={data.hotDramas} label={i18n.t('search.hotDramas')} />
			{/if}
		</div>
	{/if}
</div>
