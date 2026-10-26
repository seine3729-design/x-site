<script lang="ts">
	import type { RankBoard } from '#lib/api/types.ts';
	import Badge from '#lib/drama/Badge.svelte';
	import { dramaPath, episodesLabel } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { compactNumber, score } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = { items: RankBoard['list']; label: string };
	let { items, label }: Props = $props();

	const i18n = getI18n();
</script>

<!-- 热门影片：榜单数据（/api/v1/rank）做成「缩略图 + 名次 + 标签 + 播放量」的两列清单，
     用户没输入关键词时填满搜索页，比只摆一排热搜词更有可点性 -->
<section class="rounded-panel border border-line bg-surface p-3 sm:p-5" aria-labelledby="hot-dramas-title">
	<h2 id="hot-dramas-title" class="mb-3 flex items-center gap-1.5 px-1 text-[15px] font-bold">
		<Icon name="flame" size={17} class="text-brand-fg" />{label}
	</h2>
	<ol class="grid gap-1 md:grid-cols-2 md:gap-x-4">
		{#each items as item (item.drama.id)}
			{@const drama = item.drama}
			{@const rating = score(drama.score)}
			<li>
				<a
					href={i18n.path(dramaPath(drama.id))}
					class="group flex items-start gap-3 rounded-2xl p-2 transition-colors hover:bg-raised"
					data-sveltekit-preload-data="hover"
				>
					<span class="relative block w-16 shrink-0 overflow-hidden rounded-card ring-1 ring-line ring-inset sm:w-[4.5rem]">
						<Poster cover={drama.cover_portrait} title={drama.title} imgClass="transition-transform duration-500 group-hover:scale-105" />
						<Badge value={drama.badge} corner class="absolute start-0 top-0" />
						<!-- 名次压在封面左下角，和图里的榜单一致 -->
						<span
							class="absolute start-1 bottom-0.5 text-[1.6rem] leading-none font-black text-white italic [text-shadow:0_2px_8px_rgb(0_0_0/0.7)]"
							aria-hidden="true">{item.rank}</span
						>
					</span>
					<span class="min-w-0 flex-1 pt-0.5">
						<span class="line-clamp-2 text-[15px] leading-snug font-semibold text-fg group-hover:text-brand-fg">{drama.title}</span>
						<span class="mt-1.5 flex flex-wrap items-center gap-1.5">
							<span class="rounded-md bg-raised px-1.5 py-0.5 text-[11px] leading-4 text-fg-muted">{episodesLabel(i18n, drama)}</span>
							{#if rating}
								<span class="rounded-md bg-raised px-1.5 py-0.5 text-[11px] leading-4 font-semibold text-vip-fg">{rating}</span>
							{/if}
							{#if drama.year}
								<span class="rounded-md bg-raised px-1.5 py-0.5 text-[11px] leading-4 text-fg-muted tabular">{drama.year}</span>
							{/if}
						</span>
						<span class="mt-1.5 flex items-center gap-1 text-xs text-fg-subtle">
							<Icon name="play" size={11} filled strokeWidth={0} />
							<span class="tabular">{compactNumber(item.heat || drama.play_count, i18n.locale)}</span>
							<span class="sr-only">{i18n.t('rank.position', { n: item.rank })}</span>
						</span>
					</span>
				</a>
			</li>
		{/each}
	</ol>
</section>
