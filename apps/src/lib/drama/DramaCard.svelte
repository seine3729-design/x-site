<script lang="ts">
	import type { DramaCard } from '#lib/api/types.ts';
	import { compactNumber, score } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { getSite } from '#lib/site.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Badge from './Badge.svelte';
	import { dramaPath, episodesLabel } from './labels.ts';
	import Poster from './Poster.svelte';

	type Props = {
		card: DramaCard;
		priority?: boolean;
		/** 标题下显示评分与年份 */
		meta?: boolean;
		/** 覆盖默认去向（如播放页的相关推荐直接进播放） */
		href?: string;
		/** 观看进度 0–1（继续观看） */
		progress?: number;
		/** 进度条下的说明（如「看到第 3 集」） */
		note?: string;
	};

	let { card, priority = false, meta = false, href, progress, note }: Props = $props();

	const i18n = getI18n();
	const rating = $derived(score(card.score));
	// 卡片标题位置由后台配（layout.card_style）：below 在封面下、overlay 压在封面上
	const overlay = $derived(getSite().layout.cardStyle === 'overlay');
</script>

<a
	href={href ?? i18n.path(dramaPath(card.id))}
	class="group block min-w-0 rounded-card outline-offset-4 transition-transform duration-100 active:scale-[0.975] motion-reduce:transition-none motion-reduce:active:scale-100"
	data-sveltekit-preload-data="hover"
>
	<div class="relative overflow-hidden rounded-card bg-sunken ring-1 ring-line ring-inset">
		<Poster
			cover={card.cover_portrait}
			title={card.title}
			{priority}
			imgClass="transition-transform duration-500 ease-out group-hover:scale-[1.05]"
		/>
		<Badge value={card.badge} class="absolute start-1.5 top-1.5 max-w-[80%]" />
		<span
			class="poster-scrim absolute inset-x-0 bottom-0 flex flex-col gap-0.5 px-2 pt-10 pb-1.5 text-[11px] leading-4 font-medium text-white"
		>
			{#if overlay}
				<!-- 标题压在封面上：白字 + 渐变遮罩，任何封面上都可读 -->
				<h3 class="line-clamp-2 text-[13px] leading-snug font-bold [text-shadow:0_1px_6px_rgb(0_0_0/0.5)]">{card.title}</h3>
			{/if}
			<span class="flex items-end justify-between gap-1">
				<span class="truncate">{episodesLabel(i18n, card)}</span>
				{#if card.play_count > 0}
					<span class="flex shrink-0 items-center gap-0.5 opacity-90">
						<Icon name="play" size={10} filled strokeWidth={0} />
						<span class="tabular">{compactNumber(card.play_count, i18n.locale)}</span>
					</span>
				{/if}
			</span>
		</span>
		{#if progress !== undefined}
			<span class="absolute inset-x-0 bottom-0 h-[3px] bg-white/25" aria-hidden="true">
				<span class="block h-full bg-brand" style:width="{Math.round(Math.min(1, Math.max(0, progress)) * 100)}%"></span>
			</span>
		{/if}
		<!-- 电脑端悬停时的播放提示 -->
		<span
			class="pointer-events-none absolute inset-0 hidden place-items-center bg-black/25 opacity-0 transition-opacity duration-200 group-hover:opacity-100 lg:grid"
			aria-hidden="true"
		>
			<span class="grid size-12 place-items-center rounded-full bg-brand/90 text-white shadow-lg backdrop-blur">
				<Icon name="play" size={20} filled strokeWidth={0} class="translate-x-px" />
			</span>
		</span>
	</div>
	{#if !overlay}
		<h3 class="mt-2 line-clamp-2 text-[14px] leading-snug font-semibold text-fg transition-colors group-hover:text-brand-fg">
			{card.title}
		</h3>
	{/if}
	{#if note}
		<p class="mt-0.5 truncate text-xs text-fg-subtle">{note}</p>
	{:else if meta && (rating || card.year)}
		<p class="mt-1 flex items-center gap-2 text-xs text-fg-subtle">
			{#if rating}
				<span class="flex items-center gap-0.5 font-semibold text-vip-fg">
					<Icon name="star" size={11} filled strokeWidth={0} />{rating}
				</span>
			{/if}
			{#if card.year}<span>{card.year}</span>{/if}
		</p>
	{/if}
</a>
