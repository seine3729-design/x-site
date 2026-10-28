<script lang="ts">
	import type { DramaCard } from '#lib/api/types.ts';
	import Badge from '#lib/drama/Badge.svelte';
	import { dramaPath, episodesLabel, playPath } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { compactNumber, score } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = { dramas: DramaCard[] };
	let { dramas }: Props = $props();

	const i18n = getI18n();
</script>

<!--
  精选大卡：竖封面 + 信息，手机一列、电脑两列。
  以前标题顶到上面、按钮用 mt-auto 压到底，中间空出一大片（卡片宽 500px 却只有三行字）；
  现在右栏整体居中、元信息压成一排小标签、按钮跟在信息后面，卡高跟着封面走，没有空档。
-->
<ul class="grid gap-2.5 md:grid-cols-2 lg:gap-4">
	{#each dramas.slice(0, 4) as drama (drama.id)}
		{@const rating = score(drama.score)}
		<li
			class="group relative overflow-hidden rounded-panel border border-line bg-surface transition-colors hover:border-line-strong"
		>
			<!-- 封面抽出来的氛围底色，hover 时稍微提亮 -->
			<div class="absolute inset-0 opacity-25 blur-3xl transition-opacity duration-500 group-hover:opacity-40" aria-hidden="true">
				<Poster cover={drama.cover_portrait} title={drama.title} class="absolute! inset-0 size-full" decorative blurred />
			</div>
			<div class="relative flex gap-3.5 p-3">
				<a
					href={i18n.path(dramaPath(drama.id))}
					class="relative w-24 shrink-0 overflow-hidden rounded-card ring-1 ring-line ring-inset lg:w-32"
					tabindex="-1"
					aria-hidden="true"
				>
					<Poster cover={drama.cover_portrait} title={drama.title} imgClass="transition-transform duration-500 group-hover:scale-[1.04]" />
					<span
						class="pointer-events-none absolute inset-0 hidden place-items-center bg-black/25 opacity-0 transition-opacity duration-200 group-hover:opacity-100 lg:grid"
						aria-hidden="true"
					>
						<span class="grid size-9 place-items-center rounded-full bg-brand/90 text-white shadow-lg backdrop-blur">
							<Icon name="play" size={15} filled strokeWidth={0} class="translate-x-px" />
						</span>
					</span>
				</a>

				<div class="flex min-w-0 flex-1 flex-col justify-center gap-2">
					<Badge value={drama.badge} size="md" />
					<h3 class="line-clamp-2 text-[15px] leading-snug font-bold lg:text-xl">
						<a href={i18n.path(dramaPath(drama.id))} class="transition-colors hover:text-brand-fg">{drama.title}</a>
					</h3>

					<p class="flex flex-wrap items-center gap-1.5 text-[11px] leading-4 font-medium">
						{#if rating}
							<span class="flex items-center gap-0.5 rounded-md bg-vip/15 px-1.5 py-0.5 font-bold text-vip-fg">
								<Icon name="star" size={10} filled strokeWidth={0} />{rating}
							</span>
						{/if}
						<span class="rounded-md bg-raised/80 px-1.5 py-0.5 text-fg-muted">{episodesLabel(i18n, drama)}</span>
						{#if drama.play_count > 0}
							<span class="flex items-center gap-0.5 rounded-md bg-raised/80 px-1.5 py-0.5 text-fg-muted">
								<Icon name="play" size={9} filled strokeWidth={0} />
								<span class="tabular">{compactNumber(drama.play_count, i18n.locale)}</span>
							</span>
						{/if}
						<!-- 年份在手机上藏起来：390px 宽放四个标签会折成两行，把卡片顶高一截 -->
						{#if drama.year}
							<span class="hidden rounded-md bg-raised/80 px-1.5 py-0.5 tabular text-fg-muted sm:inline">{drama.year}</span>
						{/if}
					</p>

					<div class="flex items-center gap-2">
						{#if drama.latest_episode > 0}
							<a href={i18n.path(playPath(drama.id, 1))} class="btn btn-sm btn-primary gap-1.5">
								<Icon name="play" size={14} filled strokeWidth={0} />
								{i18n.t('drama.playNow')}
							</a>
						{/if}
						<a href={i18n.path(dramaPath(drama.id))} class="btn btn-sm btn-ghost gap-1.5">
							{i18n.t('drama.details')}
							<Icon name="chevron-right" size={14} class="rtl:rotate-180" />
						</a>
					</div>
				</div>
			</div>
		</li>
	{/each}
</ul>
