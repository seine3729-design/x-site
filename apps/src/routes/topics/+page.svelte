<script lang="ts">
	import { page } from '$app/state';
	import type { CursorPage, TopicCard } from '#lib/api/types.ts';
	import TopicCover from '#lib/home/TopicCover.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';

	let { data } = $props();
	const i18n = getI18n();

	let extra = $state<TopicCard[]>([]);
	let cursor = $state<string | null>(null);
	let loading = $state(false);
	$effect.pre(() => {
		cursor = data.topics.next_cursor;
		extra = [];
	});
	const topics = $derived([...data.topics.list, ...extra]);

	async function more() {
		if (!cursor || loading) {
			return;
		}
		loading = true;
		try {
			const { api } = await import('#lib/api/client.ts');
			const next = await api.get<CursorPage<TopicCard>>(`/api/v1/topics?limit=30&cursor=${encodeURIComponent(cursor)}`);
			extra = [...extra, ...next.list];
			cursor = next.next_cursor;
		} finally {
			loading = false;
		}
	}
</script>

<svelte:head>
	<title>{i18n.t('topic.title')} - {page.data.site.name}</title>
	<meta name="description" content={i18n.t('topic.description')} />
</svelte:head>

<div class="page-x pt-4 lg:pt-8">
	<h1 class="mb-1 text-2xl font-extrabold tracking-tight lg:text-3xl">{i18n.t('topic.title')}</h1>
	<p class="mb-6 text-sm text-fg-muted">{i18n.t('topic.description')}</p>

	{#if topics.length === 0}
		<EmptyState icon="layers" title={i18n.t('topic.empty')}>
			<a href={i18n.path('/explore')} class="btn btn-ghost">{i18n.t('home.goExplore')}</a>
		</EmptyState>
	{:else}
		<ul class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
			{#each topics as topic, index (topic.id)}
				{@const extra = data.covers[topic.slug]}
				<li>
					<a
						href={i18n.path(`/topics/${encodeURIComponent(topic.slug)}`)}
						class="group block overflow-hidden rounded-panel border border-line bg-surface transition-colors hover:border-line-strong"
						data-sveltekit-preload-data="hover"
					>
						<span class="relative block aspect-[16/9] overflow-hidden">
							<span class="absolute inset-0 transition-transform duration-500 group-hover:scale-[1.04]">
								<TopicCover title={topic.title} covers={extra?.covers} priority={index < 3} />
							</span>
							<!-- 文字压在图上：底部加深，任何封面上白字都读得清 -->
							<span class="absolute inset-0 bg-gradient-to-t from-black/85 via-black/35 to-black/10"></span>
							<span class="absolute inset-x-0 bottom-0 flex items-end justify-between gap-3 p-4 text-white">
								<span class="min-w-0">
									<span class="line-clamp-2 text-lg leading-tight font-extrabold tracking-tight [text-shadow:0_2px_10px_rgb(0_0_0/0.5)]">
										{topic.title}
									</span>
									{#if extra && extra.count > 0}
										<span class="mt-1.5 inline-flex items-center gap-1 rounded-full bg-white/15 px-2 py-0.5 text-[11px] font-semibold backdrop-blur">
											<Icon name="layers" size={11} />{i18n.t('topic.count', { n: extra.count })}
										</span>
									{/if}
								</span>
								<span
									class="grid size-9 shrink-0 place-items-center rounded-full bg-white/15 backdrop-blur transition-colors group-hover:bg-brand group-hover:text-on-brand"
								>
									<Icon name="chevron-right" size={18} class="rtl:rotate-180" />
								</span>
							</span>
						</span>
						{#if topic.summary}
							<span class="block px-4 py-3.5 text-[13px] leading-6 text-fg-muted">
								<span class="line-clamp-2">{topic.summary}</span>
							</span>
						{/if}
					</a>
				</li>
			{/each}
		</ul>
		{#if cursor}
			<div class="flex justify-center py-8">
				<button type="button" class="btn btn-sm btn-ghost" disabled={loading} onclick={more}>
					{i18n.t(loading ? 'common.loading' : 'common.loadMore')}
				</button>
			</div>
		{/if}
	{/if}
</div>
