<script lang="ts">
	import BlobImage from '#lib/media/BlobImage.svelte';
	import { imageSources, titleHue } from '#lib/media/image.ts';
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
	};

	let {
		cover,
		title,
		shape = 'portrait',
		priority = false,
		class: className = '',
		imgClass = '',
		decorative = false
	}: Props = $props();

	const site = $derived(getSite());
	const sources = $derived(imageSources(cover, site.imageBases));
	const hue = $derived(titleHue(title));
	// 宽高只用来占位（BlobImage 按它写 aspect-ratio，避免 CLS），不是实际请求尺寸
	const size = $derived(shape === 'portrait' ? coverSize(site.theme.coverRatio) : { width: 640, height: 360 });
</script>

{#snippet placeholder()}
	<!-- 没有封面或图片线路都失败时：按剧名生成稳定的渐变底 + 剧名，列表里不会出现一块块灰色 -->
	<!-- 剧名放在中上部：卡片底部还要叠集数、播放量，两者不能撞在一起 -->
	<span
		class="@container absolute inset-0 flex flex-col items-center justify-center px-3 pb-[22%] text-center"
		style:background="radial-gradient(120% 80% at 20% 0%, hsl({hue} 65% 34%) 0%, transparent 60%), linear-gradient(160deg, hsl({(hue +
			40) %
			360} 55% 22%), hsl({hue} 45% 9%))"
	>
		{#if !decorative}
			<svg viewBox="0 0 24 24" class="mb-2 size-7 fill-white/20" aria-hidden="true"
				><path d="M7 4.8v14.4a1 1 0 0 0 1.5.86l11.6-7.2a1 1 0 0 0 0-1.72L8.5 3.94A1 1 0 0 0 7 4.8Z" /></svg
			>
		{/if}
		<span
			class="line-clamp-3 font-extrabold tracking-tight text-balance text-white/90 [text-shadow:0_2px_12px_rgb(0_0_0/0.45)] {decorative
				? 'hidden'
				: ''} {shape === 'portrait' ? 'text-[clamp(12px,4.2cqi,17px)] leading-snug' : 'text-2xl leading-tight'}"
			aria-hidden="true">{title}</span
		>
	</span>
{/snippet}

<BlobImage
	src={sources}
	alt={title}
	width={size.width}
	height={size.height}
	{priority}
	class={className}
	{imgClass}
	fallback={placeholder}
/>
