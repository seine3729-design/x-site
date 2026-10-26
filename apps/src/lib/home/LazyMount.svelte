<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = { children: Snippet; minHeight?: string };
	let { children, minHeight = '18rem' }: Props = $props();

	// 首屏以下的区块：数据随页面下发，组件进入可视区 800px 内才挂载（docs/04 B.2），
	// 长首页的首次渲染与水合只做看得见的部分。占位有最小高度，挂载发生在视口外，不计入 CLS。
	let mounted = $state(false);
	let anchor = $state<HTMLDivElement>();

	$effect(() => {
		const el = anchor;
		if (!el || mounted) {
			return;
		}
		if (typeof IntersectionObserver === 'undefined') {
			mounted = true;
			return;
		}
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((e) => e.isIntersecting)) {
					mounted = true;
					observer.disconnect();
				}
			},
			{ rootMargin: '800px 0px' }
		);
		observer.observe(el);
		return () => observer.disconnect();
	});
</script>

{#if mounted}
	{@render children()}
{:else}
	<div bind:this={anchor} style:min-height={minHeight} aria-hidden="true"></div>
{/if}
