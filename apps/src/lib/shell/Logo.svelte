<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import BlobImage from '#lib/media/BlobImage.svelte';
	import { imageSources } from '#lib/media/image.ts';
	import { getSite } from '#lib/site.ts';

	type Props = { class?: string; showName?: boolean };
	let { class: className = '', showName = true }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());
	const logo = $derived(imageSources(site.logo, site.imageBases));
</script>

{#snippet mark()}
	<!-- 内置标识：品牌色渐变圆角方块 + 播放三角，颜色跟着后台配的主色走 -->
	<!-- 渐变与投影写在 app.css 的 .brand-mark：那里能给 color-mix 配一条回退声明，
	     内联 style 里写两次同名属性会被 svelte/no-dupe-style-properties 拦下 -->
	<span class="brand-mark grid size-8 place-items-center rounded-[10px]" aria-hidden="true">
		<svg viewBox="0 0 24 24" width="16" height="16" class="translate-x-px fill-white"
			><path d="M7 4.8v14.4a1 1 0 0 0 1.5.86l11.6-7.2a1 1 0 0 0 0-1.72L8.5 3.94A1 1 0 0 0 7 4.8Z" /></svg
		>
	</span>
{/snippet}

<a href={i18n.path('/')} class="flex shrink-0 items-center gap-2.5 {className}" aria-label={site.name}>
	{#if logo.length > 0}
		<BlobImage src={logo} alt="" width={32} height={32} priority class="size-8 rounded-[10px]" fallback={mark} />
	{:else}
		{@render mark()}
	{/if}
	{#if showName}
		<span class="text-[17px] font-extrabold tracking-tight text-fg">{site.name}</span>
	{/if}
</a>
