<script lang="ts">
	import type { DramaEpisode } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import { playPath } from './labels.ts';

	type Props = {
		dramaId: number;
		episodes: DramaEpisode[];
		/** 当前播放的集号（播放页高亮） */
		current?: number;
		/** 每个分组的集数；超过一组时显示分组标签（1-50、51-100…） */
		groupSize?: number;
		/** 紧凑：播放页侧栏与选集面板用 */
		dense?: boolean;
		onpick?: (episode: DramaEpisode) => void;
	};

	let { dramaId, episodes, current, groupSize = 50, dense = false, onpick }: Props = $props();

	const i18n = getI18n();
	const groups = $derived(Math.max(1, Math.ceil(episodes.length / groupSize)));
	let picked = $state<number | null>(null);
	// 默认打开当前集所在的分组
	const currentIndex = $derived(current === undefined ? -1 : episodes.findIndex((e) => e.number === current));
	const group = $derived(picked ?? (currentIndex >= 0 ? Math.floor(currentIndex / groupSize) : 0));
	const visible = $derived(episodes.slice(group * groupSize, (group + 1) * groupSize));

	const rangeLabel = (index: number) => {
		const from = episodes[index * groupSize]?.number ?? index * groupSize + 1;
		const to = episodes[Math.min(episodes.length, (index + 1) * groupSize) - 1]?.number ?? from;
		return `${from}-${to}`;
	};
</script>

{#if groups > 1}
	<div class="no-scrollbar relative -mx-1 mb-3 flex gap-2 overflow-x-auto px-1" role="tablist" aria-label={i18n.t('drama.episodes')}>
		{#each { length: groups }, index (index)}
			<button
				type="button"
				role="tab"
				class="chip"
				aria-selected={index === group}
				onclick={() => (picked = index)}
			>
				{rangeLabel(index)}
			</button>
		{/each}
	</div>
{/if}

<ul class="grid gap-2 {dense ? 'grid-cols-5 sm:grid-cols-6' : 'grid-cols-[repeat(auto-fill,minmax(3.75rem,1fr))] lg:grid-cols-[repeat(auto-fill,minmax(4.5rem,1fr))]'}">
	{#each visible as episode (episode.id)}
		{@const isCurrent = episode.number === current}
		<li>
			{#if episode.playable}
				<a
					href={i18n.path(playPath(dramaId, episode.number))}
					class="relative flex h-12 items-center justify-center rounded-xl text-[15px] font-semibold tabular transition-colors {isCurrent
						? 'bg-brand text-on-brand'
						: 'bg-raised text-fg hover:bg-overlay hover:text-brand-fg'}"
					aria-current={isCurrent ? 'true' : undefined}
					aria-label={i18n.t('episode.label', { n: episode.number }) +
						(episode.access === 'vip' ? ` · ${i18n.t('episode.vip')}` : episode.access === 'login' ? ` · ${i18n.t('episode.login')}` : '')}
					data-sveltekit-replacestate={current !== undefined ? '' : undefined}
					onclick={() => onpick?.(episode)}
				>
					{#if isCurrent}
						<span class="flex items-end gap-[2px]" aria-hidden="true">
							<span class="eq h-3 w-[3px] rounded-full bg-current"></span>
							<span class="eq h-3 w-[3px] rounded-full bg-current [animation-delay:-0.4s]"></span>
							<span class="eq h-3 w-[3px] rounded-full bg-current [animation-delay:-0.2s]"></span>
						</span>
					{:else}
						{episode.number}
					{/if}
					{#if episode.access === 'vip'}
						<span
							class="absolute -end-px -top-px rounded-se-xl rounded-es-lg bg-gradient-to-r from-[#f7d27a] to-[#e3a93a] px-1 text-[9px] leading-[14px] font-extrabold text-on-vip"
							>VIP</span
						>
					{:else if episode.access === 'login'}
						<span class="absolute end-1 top-1 text-fg-subtle"><Icon name="lock" size={10} strokeWidth={2.5} /></span>
					{/if}
				</a>
			{:else}
				<span
					class="flex h-12 flex-col items-center justify-center rounded-xl bg-raised/50 text-[13px] leading-4 text-fg-subtle"
					title={i18n.t('episode.soon')}
				>
					<span class="tabular">{episode.number}</span>
					<span class="text-[9px]">{i18n.t('episode.soonShort')}</span>
				</span>
			{/if}
		</li>
	{/each}
</ul>

<style>
	.eq {
		animation: eq 0.9s ease-in-out infinite;
		transform-origin: bottom;
	}
	@keyframes eq {
		0%,
		100% {
			transform: scaleY(0.35);
		}
		50% {
			transform: scaleY(1);
		}
	}
</style>
