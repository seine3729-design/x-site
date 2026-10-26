<script lang="ts">
	import type { FacetTaxonomy } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { exploreSearch, toggleTerm, WINDOWS, type ExploreQuery } from './query.ts';

	type Props = { query: ExploreQuery; taxonomies: FacetTaxonomy[]; basePath: string; onpick?: () => void };
	let { query, taxonomies, basePath, onpick }: Props = $props();

	const i18n = getI18n();
	const href = (next: ExploreQuery) => i18n.path(basePath) + exploreSearch(next);

	const WINDOW_KEYS = {
		all: 'explore.windowAll',
		'7d': 'explore.window7d',
		'14d': 'explore.window14d',
		'30d': 'explore.window30d',
		'90d': 'explore.window90d'
	} as const;
</script>

<!-- 每一行是一个筛选维度；全部用链接实现，不依赖 JS，也能被分享和后退 -->
<div class="flex flex-col gap-3 text-sm">
	{#each taxonomies as taxonomy (taxonomy.id)}
		{@const selected = query.terms.filter((t) => t.startsWith(`${taxonomy.slug}:`))}
		<div class="flex items-start gap-3">
			<span class="w-14 shrink-0 pt-1.5 text-[13px] text-fg-subtle">{taxonomy.name}</span>
			<ul class="no-scrollbar relative -me-4 flex flex-1 gap-1.5 overflow-x-auto pe-4 md:me-0 md:flex-wrap md:overflow-visible md:pe-0">
				<li>
					<a
						href={href(toggleTerm(query, taxonomy.slug, null, taxonomy.multi_select))}
						class="chip"
						aria-current={selected.length === 0 ? 'page' : undefined}
						data-sveltekit-noscroll
						data-sveltekit-keepfocus
						onclick={onpick}>{i18n.t('common.all')}</a
					>
				</li>
				{#each taxonomy.terms as term (term.id)}
					{@const key = `${taxonomy.slug}:${term.slug}`}
					<li>
						<a
							href={href(toggleTerm(query, taxonomy.slug, term.slug, taxonomy.multi_select))}
							class="chip"
							aria-current={selected.includes(key) ? 'page' : undefined}
							data-sveltekit-noscroll
							data-sveltekit-keepfocus
							onclick={onpick}>{term.name}</a
						>
					</li>
				{/each}
			</ul>
		</div>
	{/each}

	<div class="flex items-start gap-3">
		<span class="w-14 shrink-0 pt-1.5 text-[13px] text-fg-subtle">{i18n.t('explore.status')}</span>
		<ul class="flex flex-1 flex-wrap gap-1.5">
			{#each [['', 'common.all'], ['true', 'explore.finished'], ['false', 'explore.ongoing']] as const as [value, key] (value)}
				<li>
					<a
						href={href({ ...query, finished: value })}
						class="chip"
						aria-current={query.finished === value ? 'page' : undefined}
						data-sveltekit-noscroll
						data-sveltekit-keepfocus
						onclick={onpick}>{i18n.t(key)}</a
					>
				</li>
			{/each}
		</ul>
	</div>

	<div class="flex items-start gap-3">
		<span class="w-14 shrink-0 pt-1.5 text-[13px] text-fg-subtle">{i18n.t('explore.window')}</span>
		<ul class="flex flex-1 flex-wrap gap-1.5">
			{#each WINDOWS as value (value)}
				<li>
					<a
						href={href({ ...query, window: value })}
						class="chip"
						aria-current={query.window === value ? 'page' : undefined}
						data-sveltekit-noscroll
						data-sveltekit-keepfocus
						onclick={onpick}>{i18n.t(WINDOW_KEYS[value])}</a
					>
				</li>
			{/each}
		</ul>
	</div>
</div>
