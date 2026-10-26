<script lang="ts">
	import Poster from '#lib/drama/Poster.svelte';
	import { titleHue } from '#lib/media/image.ts';

	type Props = {
		title: string;
		/** 专题里前几部剧的竖版封面；为空时用按标题生成的渐变底 */
		covers?: string[];
		priority?: boolean;
	};
	let { title, covers = [], priority = false }: Props = $props();

	const hue = $derived(titleHue(title));
	const shown = $derived(covers.slice(0, 3));
</script>

<!-- 专题配图：用专题里前三部剧的封面拼成三联，没有封面时退回按标题生成的稳定渐变（同一专题每次颜色一样） -->
<span class="absolute inset-0 block" aria-hidden="true">
	{#if shown.length > 0}
		<!-- 主图占三分之二，剩下两张在右侧上下排：比三张等宽并排少一道硬接缝，更像编辑拼贴 -->
		<span class="flex h-full w-full gap-px">
			<span class="relative block h-full flex-[2] overflow-hidden">
				<Poster cover={shown[0]!} {title} {priority} class="absolute! inset-0 size-full" />
			</span>
			{#if shown.length > 1}
				<span class="flex h-full flex-1 flex-col gap-px">
					{#each shown.slice(1) as cover, index (index)}
						<span class="relative block flex-1 overflow-hidden">
							<Poster {cover} title="" class="absolute! inset-0 size-full" />
						</span>
					{/each}
				</span>
			{/if}
		</span>
	{:else}
		<span
			class="block h-full w-full"
			style:background="radial-gradient(90% 120% at 100% 0%, hsl({hue} 70% 45% / 0.85), transparent 60%), linear-gradient(135deg, hsl({(hue +
				200) %
				360} 45% 20%), hsl({hue} 40% 12%))"
		></span>
	{/if}
</span>
