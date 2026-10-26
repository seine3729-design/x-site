<script lang="ts">
	import type { TopicCard } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { titleHue } from '#lib/media/image.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = { topics: TopicCard[] };
	let { topics }: Props = $props();

	const i18n = getI18n();
</script>

<!--
  专题卡。手机一屏露出约 1.8 张（以前是 72% 宽 + 128px 高，一屏只看到一张多，
  简介还会折行后被截成「剧荒必备。」孤零零一行）；标题与简介在手机上各占一行，不折行。
  首页的专题区块接口只给标题和简介（没有封面列表），所以配图是按标题生成的稳定渐变，
  和专题列表页的封面拼贴不同——那边是 BFF 额外聚合来的。
-->
<ul
	class="no-scrollbar relative -mx-4 flex snap-x snap-mandatory scroll-px-4 gap-2.5 overflow-x-auto px-4 md:-mx-6 md:scroll-px-6 md:px-6 lg:mx-0 lg:grid lg:grid-cols-4 lg:gap-4 lg:px-0"
>
	{#each topics as topic (topic.id)}
		{@const hue = titleHue(topic.title)}
		<li class="w-[58%] shrink-0 snap-start sm:w-[42%] md:w-[32%] lg:w-auto">
			<a
				href={i18n.path(`/topics/${encodeURIComponent(topic.slug)}`)}
				class="group relative flex h-28 flex-col justify-end overflow-hidden rounded-panel p-3.5 text-white ring-1 ring-white/10 ring-inset transition-shadow duration-300 hover:shadow-pop sm:h-32 lg:h-40 lg:p-4"
				style:background="radial-gradient(90% 120% at 100% 0%, hsl({hue} 70% 45% / 0.85), transparent 60%), linear-gradient(135deg, hsl({(hue +
					200) %
					360} 45% 20%), hsl({hue} 40% 12%))"
			>
				<Icon
					name="layers"
					size={44}
					strokeWidth={1.2}
					class="absolute end-2.5 top-2.5 text-white/14 transition-transform duration-500 group-hover:scale-110 lg:end-3 lg:top-3 lg:size-14"
				/>
				<!-- 底部压暗一档，渐变亮的时候白字也读得清 -->
				<span class="absolute inset-x-0 bottom-0 h-3/5 bg-gradient-to-t from-black/45 to-transparent" aria-hidden="true"></span>
				<span class="relative line-clamp-1 text-[15px] font-bold tracking-tight lg:text-base">{topic.title}</span>
				{#if topic.summary}
					<span class="relative mt-1 line-clamp-1 text-[11px] leading-4 text-white/75 lg:line-clamp-2 lg:text-xs lg:leading-5">
						{topic.summary}
					</span>
				{/if}
				<span class="relative mt-1.5 flex items-center gap-0.5 text-[11px] font-semibold text-white/85">
					{i18n.t('common.viewDetail')}
					<Icon name="chevron-right" size={12} class="rtl:rotate-180" />
				</span>
			</a>
		</li>
	{/each}
</ul>
