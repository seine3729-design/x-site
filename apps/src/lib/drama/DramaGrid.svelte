<script lang="ts">
	import type { DramaCard as Card } from '#lib/api/types.ts';
	import DramaCard from './DramaCard.svelte';

	type Props = {
		cards: Card[];
		/** 电脑端（≥1024px）列数，来自区块配置 cols_desktop */
		cols?: number;
		/** 前几张立即加载（首屏） */
		priorityCount?: number;
		meta?: boolean;
	};

	let { cards, cols = 6, priorityCount = 0, meta = true }: Props = $props();

	// Tailwind 只认完整类名：列数映射到写死的类
	const COLS: Record<number, string> = {
		4: 'lg:grid-cols-4',
		5: 'lg:grid-cols-5',
		6: 'lg:grid-cols-6',
		7: 'lg:grid-cols-6 xl:grid-cols-7',
		8: 'lg:grid-cols-6 xl:grid-cols-8'
	};
	const lg = $derived(COLS[Math.min(8, Math.max(4, cols))] ?? COLS[6]);
</script>

<ul class="grid grid-cols-3 gap-x-2.5 gap-y-5 sm:grid-cols-4 sm:gap-x-3 md:grid-cols-5 lg:gap-x-4 lg:gap-y-6 {lg}">
	{#each cards as card, index (card.id)}
		<li class="min-w-0">
			<DramaCard {card} priority={index < priorityCount} {meta} />
		</li>
	{/each}
</ul>
