<script lang="ts">
	import type { EmojiPack } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import BlobImage from '#lib/media/BlobImage.svelte';
	import { imageSources } from '#lib/media/image.ts';
	import { getSite } from '#lib/site.ts';

	type Props = {
		packs: EmojiPack[] | null;
		/** 点中一个表情：把表情码插进草稿 */
		onpick: (code: string) => void;
	};
	let { packs, onpick }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());
	const hasAny = $derived((packs ?? []).some((pack) => pack.emojis.length > 0));
</script>

<div class="max-h-56 overflow-y-auto rounded-xl border border-line bg-raised p-3">
	{#if packs === null}
		<p class="py-4 text-center text-sm text-fg-subtle" role="status">{i18n.t('common.loading')}</p>
	{:else if !hasAny}
		<p class="py-4 text-center text-sm text-fg-subtle">{i18n.t('comment.emojiEmpty')}</p>
	{:else}
		{#each packs.filter((pack) => pack.emojis.length > 0) as pack (pack.id)}
			<p class="mb-2 text-xs font-semibold text-fg-subtle">{pack.name}</p>
			<ul class="mb-3 flex flex-wrap gap-1.5">
				{#each pack.emojis as emoji (emoji.code)}
					<li>
						<!-- 触摸目标 44×44：表情格子本身 36px，外加内边距到 44（ui-ux touch-target-size） -->
						<button
							type="button"
							class="flex size-11 items-center justify-center rounded-lg text-xs text-fg-muted hover:bg-overlay"
							title={emoji.code}
							onclick={() => onpick(emoji.code)}
						>
							{#if imageSources(emoji.image, site.imageBases).length > 0}
								<BlobImage
									src={imageSources(emoji.image, site.imageBases)}
									alt={emoji.code}
									width={28}
									height={28}
									class="size-7 bg-transparent!"
								>
									{#snippet fallback()}<span class="sr-only">{emoji.code}</span>{/snippet}
								</BlobImage>
							{:else}
								<span class="truncate px-1">{emoji.code}</span>
							{/if}
						</button>
					</li>
				{/each}
			</ul>
		{/each}
	{/if}
</div>
