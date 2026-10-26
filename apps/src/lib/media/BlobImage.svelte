<script lang="ts">
	import { onDestroy, type Snippet } from 'svelte';
	import { loadBlobUrl } from './blob-image.ts';

	type Props = {
		/** 图片地址；传数组时按顺序作为不同线路的候选，空数组直接显示 fallback */
		src: string | readonly string[];
		alt: string;
		width: number;
		height: number;
		/** 首屏图片（前 4 张）设为 true：立即加载且高优先级，其余进入可视区附近才加载 */
		priority?: boolean;
		class?: string;
		imgClass?: string;
		/** 没有地址或全部线路都失败时显示（如带剧名的渐变占位图） */
		fallback?: Snippet;
	};

	let {
		src,
		alt,
		width,
		height,
		priority = false,
		class: className = '',
		imgClass = '',
		fallback
	}: Props = $props();

	let holder = $state<HTMLSpanElement>();
	let img = $state<HTMLImageElement>();
	let url = $state<string | null>(null);
	let failed = $state(false);
	/** 图片已解码、可以显示：解码前就淡入会闪一下（浏览器要先把帧准备好） */
	let ready = $state(false);
	/** 已经开始取图：没进可视区的图不画骨架动画，免得一页几十个 shimmer 一起跑 */
	let started = $state(false);
	let current: string | null = null;

	const sources = $derived(typeof src === 'string' ? (src ? [src] : []) : src);

	function release() {
		if (current) {
			URL.revokeObjectURL(current);
			current = null;
		}
	}

	$effect(() => {
		const list = sources;
		const el = holder;
		if (!el) {
			return;
		}
		failed = list.length === 0;
		ready = false;
		started = false;
		if (list.length === 0) {
			return;
		}
		const controller = new AbortController();

		const start = () => {
			started = true;
			loadBlobUrl(list, { signal: controller.signal, priority: priority ? 'high' : 'auto' })
				.then((next) => {
					release();
					current = next;
					url = next;
				})
				.catch(() => {
					if (!controller.signal.aborted) {
						failed = true;
					}
				});
		};

		let observer: IntersectionObserver | undefined;
		if (priority || typeof IntersectionObserver === 'undefined') {
			start();
		} else {
			observer = new IntersectionObserver(
				(entries) => {
					if (entries.some((e) => e.isIntersecting)) {
						observer?.disconnect();
						start();
					}
				},
				{ rootMargin: '300px' }
			);
			observer.observe(el);
		}

		return () => {
			controller.abort();
			observer?.disconnect();
		};
	});

	// 等解码完成再显示：decode() 失败（取消、格式坏）也照常显示，交给 <img> 的 onerror 兜底
	$effect(() => {
		const el = img;
		if (!el || !url) {
			return;
		}
		let alive = true;
		const show = () => {
			if (alive) {
				ready = true;
			}
		};
		if (typeof el.decode === 'function') {
			el.decode().then(show, show);
		} else {
			show();
		}
		return () => {
			alive = false;
		};
	});

	onDestroy(release);
</script>

<!--
  固定宽高比占位，图片出来前后布局不跳动（CLS）。
  渲染顺序：底层一直是占位层（骨架或 fallback），图片解码完成后再淡入盖上去——
  占位层不提前拿掉，所以不会出现「背景色闪一下」。
-->
<span
	bind:this={holder}
	class="relative block overflow-hidden bg-sunken {className}"
	style:aspect-ratio="{width} / {height}"
	data-state={ready ? 'loaded' : failed ? 'failed' : 'loading'}
>
	<!--
	  占位层：有 fallback（带剧名的渐变图）就一路用它垫着——不管是还没进可视区、正在取图、还是取失败，
	  位置上都是一张像样的图，不会出现一块空色块（实测首页「编辑精选」在图没下来前就是两块纯色）。
	  没有 fallback 的地方（广告图等）仍然用骨架，且只在真的开始取图之后才动，免得满屏 shimmer。
	-->
	<span
		class="absolute inset-0 transition-opacity duration-500 ease-out-soft {ready ? 'opacity-0' : 'opacity-100'}"
		aria-hidden="true"
	>
		{#if fallback}
			{@render fallback()}
		{:else if started && !failed}
			<span class="skeleton block size-full"></span>
		{/if}
	</span>

	{#if url}
		<img
			bind:this={img}
			src={url}
			{alt}
			{width}
			{height}
			decoding="async"
			class="absolute inset-0 size-full object-cover transition-opacity duration-500 ease-out-soft {ready
				? 'opacity-100'
				: 'opacity-0'} {imgClass}"
			onerror={() => {
				failed = true;
				ready = false;
			}}
		/>
	{:else if !failed}
		<span class="sr-only">{alt}</span>
	{/if}
</span>
