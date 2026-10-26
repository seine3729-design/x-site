<script lang="ts">
	import type { DramaCard } from '#lib/api/types.ts';
	import { dramaPath, episodesLabel } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { compactNumber } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Rail from '#lib/ui/Rail.svelte';

	type Props = { dramas: DramaCard[]; label: string };
	let { dramas, label }: Props = $props();

	const i18n = getI18n();
	const top = $derived(dramas.slice(0, 10));
</script>

<!-- 前十名：名次是压在海报左下角的大号白色描边斜体数字（主流短剧站的「Top 10」样式）。
     名次曾经排在海报外侧，那样它比海报还高、还要占掉几十像素宽，海报被挤窄、行距也被撑大。 -->
<Rail items={top} key={(d) => d.id} {label}>
	{#snippet item(drama, index)}
		<a href={i18n.path(dramaPath(drama.id))} class="group block" data-sveltekit-preload-data="hover">
			<span class="relative block overflow-hidden rounded-card ring-1 ring-line ring-inset">
				<Poster
					cover={drama.cover_portrait}
					title={drama.title}
					priority={index < 4}
					imgClass="transition-transform duration-500 group-hover:scale-[1.05]"
				/>
				<!-- 底部压暗，保证名次与播放量在任何封面上都看得清 -->
				<span class="absolute inset-x-0 bottom-0 h-2/5 bg-gradient-to-t from-black/85 to-transparent" aria-hidden="true"></span>
				<!-- 两位数收窄一档，免得和右下角的播放量撞在一起 -->
				<span
					class="rank-number absolute start-0.5 -bottom-0 font-black italic select-none {index === 9
						? 'text-[2.75rem] lg:text-[3.25rem]'
						: 'text-[3.5rem] lg:text-[4.25rem]'}"
					aria-hidden="true">{index + 1}</span
				>
				{#if drama.play_count > 0}
					<span class="absolute end-1.5 bottom-1.5 flex items-center gap-0.5 text-[11px] font-semibold text-white/90">
						<Icon name="play" size={10} filled strokeWidth={0} />
						<span class="tabular">{compactNumber(drama.play_count, i18n.locale)}</span>
					</span>
				{/if}
			</span>
			<span class="sr-only">{i18n.t('rank.position', { n: index + 1 })}</span>
			<span class="mt-2 block">
				<span class="line-clamp-1 text-sm font-semibold text-fg group-hover:text-brand-fg">{drama.title}</span>
				<span class="mt-0.5 block truncate text-xs text-fg-subtle">{episodesLabel(i18n, drama)}</span>
			</span>
		</a>
	{/snippet}
</Rail>

<style>
	/* 名次：白色粗斜体 + 一圈暗描边 + 柔和投影，贴在海报左下角压着底边。
	   描边用八方向 text-shadow 叠出来，不用 -webkit-text-stroke（它画在字形中线上会把笔画吃细）
	   也不用 paint-order（老版本 Safari / Chrome 对 HTML 文本不支持，见 docs/04 A.6）。 */
	.rank-number {
		color: #fff;
		line-height: 0.8;
		letter-spacing: -0.07em;
		text-shadow:
			1.6px 0 0 rgb(0 0 0 / 0.5),
			-1.6px 0 0 rgb(0 0 0 / 0.5),
			0 1.6px 0 rgb(0 0 0 / 0.5),
			0 -1.6px 0 rgb(0 0 0 / 0.5),
			1.2px 1.2px 0 rgb(0 0 0 / 0.5),
			-1.2px 1.2px 0 rgb(0 0 0 / 0.5),
			1.2px -1.2px 0 rgb(0 0 0 / 0.5),
			-1.2px -1.2px 0 rgb(0 0 0 / 0.5),
			0 4px 16px rgb(0 0 0 / 0.7);
	}
</style>
