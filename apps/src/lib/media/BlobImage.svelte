<script lang="ts">
	import { onDestroy, type Snippet } from 'svelte';
	import { getSite } from '#lib/site.ts';
	import { imageCache, loadBlobUrl } from './blob-image.ts';
	import { IMAGE_WIDTHS, imageCandidates, pickWidth, type ResizeRequest } from './image.ts';
	import { afterLead } from './lead.ts';

	type Props = {
		/** 图片地址；传数组时按顺序作为不同线路的候选，空数组直接显示 fallback */
		src: string | readonly string[];
		alt: string;
		/** 固定宽高比的宽度；省略时按图片原始比例自适应宽度 */
		width?: number;
		/** 图片高度；省略 width 时同时作为实际渲染高度 */
		height: number;
		/** 首屏图片（前 4 张）设为 true：立即加载且高优先级，其余进入可视区附近才加载 */
		priority?: boolean;
		/**
		 * 只作氛围底色、会被强模糊的图（如精选卡、详情页的背景）：只取最小一档，细节反正看不见。
		 * 实测精选卡的模糊底色原来按卡片宽度取了 1080 宽的整张图。
		 */
		blurred?: boolean;
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
		blurred = false,
		class: className = '',
		imgClass = '',
		fallback
	}: Props = $props();

	// 部署配置（IMAGE_RESIZE_ORIGINS），整个页面生命周期内不变：初始化时读一次，不放进响应式
	const resizeOrigins = getSite().resizeOrigins;

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
	// effect 按内容而不是数组身份重跑：父组件重新派生出一个内容相同的新数组时（如布局数据刷新），
	// 不要把正在下载的图中止掉再从头下一遍
	const sourceKey = $derived(sources.join('\n'));

	function release() {
		if (current) {
			URL.revokeObjectURL(current);
			current = null;
		}
	}

	/** 按实际显示尺寸要哪一档（docs/08 第 5 节）；量不到尺寸或缩放入口已判定不可用时返回 null，只用原图 */
	function resizeRequest(box: DOMRectReadOnly | null): ResizeRequest | null {
		if (!box || imageCache.resizeBroken) {
			return null;
		}
		if (blurred) {
			return { width: IMAGE_WIDTHS[0], format: 'webp' };
		}
		const dpr = window.devicePixelRatio || 1;
		// 省略 width（页头 Logo）时宽度随原图比例，只能按高度要
		return width === undefined
			? { height: pickWidth(box.height, dpr), format: 'webp' }
			: { width: pickWidth(box.width, dpr), format: 'webp' };
	}

	$effect(() => {
		const list = sourceKey ? sourceKey.split('\n') : [];
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

		const start = (box: DOMRectReadOnly | null) => {
			started = true;
			// 首页有轮播首图（LCP）时等它先画出来再下载，不和它抢带宽（#lib/media/lead.ts）。
			// 首屏图高优先级；其余是滚到附近才取的，降成低优先级，不和脚本抢带宽
			afterLead()
				.then(() => {
					controller.signal.throwIfAborted();
					return loadBlobUrl(imageCandidates(list, resizeRequest(box), resizeOrigins), {
						signal: controller.signal,
						priority: priority ? 'high' : 'low'
					});
				})
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

		if (typeof IntersectionObserver === 'undefined') {
			start(null);
			return () => controller.abort();
		}

		// 首屏图（priority）也等第一次交叉回调再取：回调里带着浏览器排版后量好的尺寸，
		// 不用在水合时自己去读 getBoundingClientRect 触发强制重排
		const observer = new IntersectionObserver(
			(entries) => {
				const entry = entries[entries.length - 1];
				if (!entry) {
					return;
				}
				const box = entry.boundingClientRect;
				// 没有渲染出来（display:none，如手机上电脑端专用的那张轮播图）：不取，显示出来进入可视区时会再回调
				if (box.width === 0 && box.height === 0) {
					return;
				}
				// 注意交叉面积为 0（贴边）也算相交：横滑轨道里紧挨着可视区的下一张轮播图会因此提前取，
				// 正好给自动轮播预取下一张；它排在首图之后（afterLead），不和首图抢带宽
				if (!priority && !entry.isIntersecting) {
					return;
				}
				observer.disconnect();
				start(box);
			},
			{ rootMargin: '300px' }
		);
		observer.observe(el);

		return () => {
			controller.abort();
			observer.disconnect();
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
  传入 width 时固定宽高比占位，图片出来前后布局不跳动（CLS）。
  省略 width（页头 Logo）时固定高度，宽度在图片可用后按原图比例自适应；
  此时先保留一个高度等宽的最小占位，以维持回退图标的尺寸。
  渲染顺序：底层是占位层（骨架或 fallback），图片解码完成后再淡入盖上去，所以不会出现「背景色闪一下」。
-->
<span
	bind:this={holder}
	class="relative block overflow-hidden bg-sunken {className}"
	style:aspect-ratio={width === undefined ? undefined : `${width} / ${height}`}
	style:height={width === undefined ? `${height}px` : undefined}
	style:min-width={width === undefined ? `${height}px` : undefined}
	data-state={ready ? 'loaded' : failed ? 'failed' : 'loading'}
>
	<!--
	  占位层分两种状态，不混用：
	  - **加载中一律是骨架**（全站同一种底色与扫光）。以前有 fallback 的地方（剧集封面）在加载中就铺
	    按剧名生成的渐变，于是一页里每张卡一个颜色；骨架才是「正在加载」该有的样子。
	  - **取不到图才用 fallback**（带剧名的占位）。那是内容状态而不是加载状态，让它一直闪会让人
	    以为还在加载。没有 fallback 的地方（广告、Logo）就留 holder 的平底色。
	  骨架在图片解码完成时立刻卸载：opacity 0 的元素动画照样在跑，一页几十张会白烧合成器。
	  它的底色与 holder 的 bg-sunken 是同一个令牌，卸载时看不出变化，图片就在这层平底色上淡入。
	-->
	{#if failed}
		{#if fallback}
			<span class="absolute inset-0" aria-hidden="true">{@render fallback()}</span>
		{/if}
	{:else if started && !ready}
		<span class="skeleton absolute inset-0 block" aria-hidden="true"></span>
	{/if}

	{#if url}
		<img
			bind:this={img}
			src={url}
			{alt}
			{width}
			{height}
			decoding="async"
			class="{width === undefined
				? 'block h-full w-auto object-contain'
				: 'absolute inset-0 size-full object-cover'} transition-opacity duration-500 ease-out-soft {ready
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
