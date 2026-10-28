<script lang="ts">
	import BlobImage from '#lib/media/BlobImage.svelte';
	import { imageSources } from '#lib/media/image.ts';
	import { getSite } from '#lib/site.ts';
	import { coverSize } from '#lib/theme/settings.ts';

	type Props = {
		cover: string;
		title: string;
		/** portrait 竖版封面（比例由后台 theme.cover_ratio 配，默认 2:3），landscape 16:9（横幅、轮播） */
		shape?: 'portrait' | 'landscape';
		priority?: boolean;
		class?: string;
		imgClass?: string;
		/**
		 * 装饰用（氛围底图、封面拼贴）：没有封面时只画渐变，不画剧名与播放图标。
		 * 否则同一个剧名会在页面上出现两次（实测 hero 右侧底图把标题又渲染了一遍）。
		 */
		decorative?: boolean;
		/** 会被强模糊的氛围底图：只取最小一档（见 BlobImage） */
		blurred?: boolean;
	};

	let {
		cover,
		title,
		shape = 'portrait',
		priority = false,
		class: className = '',
		imgClass = '',
		decorative = false,
		blurred = false
	}: Props = $props();

	const site = $derived(getSite());
	const sources = $derived(imageSources(cover, site.imageBases));
	// 宽高只用来占位（BlobImage 按它写 aspect-ratio，避免 CLS），不是实际请求尺寸
	const size = $derived(shape === 'portrait' ? coverSize(site.theme.coverRatio) : { width: 640, height: 360 });
</script>

{#snippet placeholder()}
	<!--
	  **取不到封面时**的占位（加载中统一是骨架，见 BlobImage）。全站同一种底色，不再按剧名生成渐变：
	  一页几十张卡各自一个颜色，和骨架不是一套视觉，运营也分不清哪张是真封面、哪张没图。
	  剧名放在中上部：卡片底部还要叠集数、播放量，两者不能撞在一起。
	-->
	<span class="@container absolute inset-0 flex flex-col items-center justify-center bg-sunken px-3 pb-[22%] text-center">
		{#if !decorative}
			<svg viewBox="0 0 24 24" class="mb-2 size-7 fill-fg-subtle/70" aria-hidden="true"
				><path d="M7 4.8v14.4a1 1 0 0 0 1.5.86l11.6-7.2a1 1 0 0 0 0-1.72L8.5 3.94A1 1 0 0 0 7 4.8Z" /></svg
			>
			<span
				class="line-clamp-3 font-bold tracking-tight text-balance text-fg-muted {shape === 'portrait'
					? 'text-[clamp(12px,4.2cqi,17px)] leading-snug'
					: 'text-2xl leading-tight'}"
				aria-hidden="true">{title}</span
			>
		{/if}
	</span>
{/snippet}

<BlobImage
	src={sources}
	alt={title}
	width={size.width}
	height={size.height}
	{priority}
	{blurred}
	class={className}
	{imgClass}
	fallback={placeholder}
/>
