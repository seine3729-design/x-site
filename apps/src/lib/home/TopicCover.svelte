<script lang="ts">
	import Poster from '#lib/drama/Poster.svelte';

	type Props = {
		title: string;
		/** 专题里前几部剧的竖版封面；为空时和剧集封面占位同一种底色 */
		covers?: string[];
		priority?: boolean;
	};
	let { title, covers = [], priority = false }: Props = $props();

	const shown = $derived(covers.slice(0, 3));
</script>

<!-- 专题配图：用专题里前三部剧的封面拼成三联（每张都走 Poster，加载中是统一骨架、无图是统一占位） -->
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
		<!-- 一部剧都没有封面：铺同一种底色，不再按标题生成渐变（理由见 #lib/drama/Poster.svelte 的占位说明） -->
		<span class="block h-full w-full bg-sunken"></span>
	{/if}
</span>
