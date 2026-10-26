<script lang="ts">
	import BlobImage from '#lib/media/BlobImage.svelte';
	import { imageSources } from '#lib/media/image.ts';
	import { getSite } from '#lib/site.ts';
	import { splitContent } from './thread.ts';

	type Props = {
		text: string;
		/** 表情码 → 图片，来自 /api/v1/emojis */
		emojis: ReadonlyMap<string, string>;
	};
	let { text, emojis }: Props = $props();

	const site = $derived(getSite());
	// 用户内容一律文本插值（禁止 {@html}）：表情码被切成独立片段，其余按纯文本渲染
	const segments = $derived(splitContent(text, emojis));
	const sourcesOf = (image: string) => imageSources(image, site.imageBases);
</script>

<!-- 长网址、长 ID 要能换行；正文保留用户敲的换行 -->
<p class="text-[15px] leading-6 break-words whitespace-pre-wrap [overflow-wrap:anywhere]">{#each segments as segment, index (index)}{#if segment.kind === 'text'}{segment.text}{:else if sourcesOf(segment.image).length > 0}<BlobImage
				src={sourcesOf(segment.image)}
				alt={segment.code}
				width={20}
				height={20}
				class="inline-block! size-5 bg-transparent! align-[-0.25em]"
			>
				{#snippet fallback()}<span class="sr-only">{segment.code}</span>{/snippet}
			</BlobImage>{:else}{segment.code}{/if}{/each}</p>
