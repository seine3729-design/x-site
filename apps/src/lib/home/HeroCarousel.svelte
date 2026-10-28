<script lang="ts">
	import { onMount } from 'svelte';
	import type { DramaCard } from '#lib/api/types.ts';
	import Badge from '#lib/drama/Badge.svelte';
	import { dramaPath, episodesLabel, playPath } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { compactNumber, score } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { imageCache } from '#lib/media/blob-image.ts';
	import { imageSources, resizedSrcset, resizedUrl } from '#lib/media/image.ts';
	import { holdImagesUntilLead, releaseAfterPaint } from '#lib/media/lead.ts';
	import { getSite } from '#lib/site.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = { dramas: DramaCard[]; label: string; priority?: boolean };
	let { dramas, label, priority = false }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());
	const slides = $derived(dramas.slice(0, 8));
	const INTERVAL_MS = 6000;

	/*
	 * 首屏第一张大图就是 LCP 元素，它不走 BlobImage（docs/08 第 5 节「LCP 超预算时首屏首图回退普通 <img>」）：
	 * BlobImage 要等脚本下载、水合完才开始 fetch，实测移动端 LCP 因此拖到 10 秒以上。
	 * 这里直接在 SSR 的 HTML 里输出 <picture>：浏览器解析到就下载（head 里还有一条 preload 更早发出），
	 * 手机竖封面、电脑横封面用 media 二选一，只下其中一张。其余几张仍是 BlobImage，滑到才取。
	 */
	const LEAD_MOBILE_WIDTHS = [360, 540, 720, 1080] as const;
	const LEAD_DESKTOP_WIDTHS = [720, 1080, 1440, 1920] as const;
	/*
	 * 手机上按 360px 宽来挑档位（实际铺满整屏，约 360–430px）：等于把像素密度封顶在 2 倍左右。
	 * 照实写 100vw 的话，412px × 1.75 倍屏（Lighthouse 的模拟机型）要 721 像素，720 档差 1 像素，
	 * 浏览器就直接挑 1080 档，字节数多六成（实测质量 70 时 84 KB → 135 KB）；轮播图上压着一层暗色渐变，2 倍密度足够。
	 * 640px 以上（平板竖屏）照实按整屏宽挑。
	 */
	const LEAD_MOBILE_SIZES = '(max-width: 639px) 360px, 100vw';
	// 电脑端轮播在 page-x 里：最宽 1440 减两侧 2rem 内边距；更窄时是视口宽减内边距（多估一点无妨）
	const LEAD_DESKTOP_SIZES = '(min-width: 90rem) 86rem, calc(100vw - 4rem)';
	const LEAD_DESKTOP_MEDIA = '(min-width: 64rem)';
	// 首图下半部压着一层很深的渐变，质量 60 肉眼看不出差别；实测 720 宽 AVIF 84 KB → 70 KB
	const LEAD_QUALITY = 60;

	const lead = $derived.by(() => {
		const first = slides[0];
		if (!priority || !first) {
			return null;
		}
		const mobile = imageSources(first.cover_portrait || first.cover_landscape, site.imageBases)[0];
		const desktop = imageSources(first.cover_landscape || first.cover_portrait, site.imageBases)[0];
		if (!mobile || !desktop) {
			return null;
		}
		const origins = site.resizeOrigins;
		return {
			mobile,
			desktop,
			mobileSrcset: resizedSrcset(mobile, LEAD_MOBILE_WIDTHS, origins, LEAD_QUALITY),
			desktopSrcset: resizedSrcset(desktop, LEAD_DESKTOP_WIDTHS, origins, LEAD_QUALITY),
			mobileSrc: resizedUrl(mobile, { width: 720, quality: LEAD_QUALITY }, origins) ?? mobile
		};
	});

	/** 缩放入口不可用（没接 Cloudflare 的域名上是 404）时退回原图；原图也失败就只留渐变占位 */
	let leadStage = $state<'resized' | 'original' | 'failed'>('resized');
	const leadResized = $derived(leadStage === 'resized' && !!lead?.mobileSrcset);
	let leadImg = $state<HTMLImageElement>();
	/** 首图画出来（或失败）后放行其余图片，见 #lib/media/lead.ts；onMount 里才拦（模块状态不能在 SSR 里动） */
	let releaseLead: (() => void) | null = null;

	function onLeadLoad() {
		const done = releaseLead;
		if (done) {
			releaseAfterPaint(leadImg, done);
		}
	}

	function onLeadError() {
		if (leadResized) {
			// 后面的 BlobImage 也别再试缩放入口了；换成原图继续拦着，等原图画出来
			imageCache.resizeBroken = true;
			leadStage = 'original';
		} else {
			leadStage = 'failed';
			releaseLead?.();
		}
	}

	let track = $state<HTMLUListElement>();
	let active = $state(0);
	let paused = $state(false);
	let reduceMotion = $state(false);
	/** 电脑端右下角的缩略图条：每张都是一次图片请求，等空闲了再挂，不占首屏带宽 */
	let showThumbs = $state(false);

	function go(index: number) {
		const el = track;
		if (!el || slides.length === 0) {
			return;
		}
		const next = (index + slides.length) % slides.length;
		const slide = el.children[next] as HTMLElement | undefined;
		if (!slide) {
			return;
		}
		// 只在轨道内横向滚动，不带动整页滚动
		const rtl = getComputedStyle(el).direction === 'rtl';
		const left = rtl ? slide.offsetLeft + slide.offsetWidth - el.clientWidth : slide.offsetLeft;
		el.scrollTo({ left, behavior: reduceMotion ? 'auto' : 'smooth' });
		active = next;
	}

	// 滚动停下时按离中线最近的一张更新当前页（手指滑动、键盘、按钮都走这里）
	function sync() {
		const el = track;
		if (!el) {
			return;
		}
		const center = el.getBoundingClientRect().left + el.clientWidth / 2;
		let best = 0;
		let distance = Infinity;
		[...el.children].forEach((child, index) => {
			const rect = child.getBoundingClientRect();
			const d = Math.abs(rect.left + rect.width / 2 - center);
			if (d < distance) {
				distance = d;
				best = index;
			}
		});
		active = best;
	}

	onMount(() => {
		if (lead && leadStage !== 'failed') {
			releaseLead = holdImagesUntilLead();
			// 首图常常在水合之前就下完了：那次 load 事件可能已经错过，这里补一次
			if (leadImg?.complete && leadImg.naturalWidth > 0) {
				onLeadLoad();
			}
		}
		reduceMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;
		const onVisibility = () => {
			paused = document.hidden;
		};
		document.addEventListener('visibilitychange', onVisibility);
		// requestIdleCallback 在 Safari 16.3 及更早没有，退回定时器（docs/04 A.6）
		const idle = typeof globalThis.requestIdleCallback === 'function' ? globalThis.requestIdleCallback : null;
		const handle = idle ? idle(() => (showThumbs = true), { timeout: 2500 }) : setTimeout(() => (showThumbs = true), 1200);
		return () => {
			document.removeEventListener('visibilitychange', onVisibility);
			if (idle) {
				globalThis.cancelIdleCallback?.(handle as number);
			} else {
				clearTimeout(handle as ReturnType<typeof setTimeout>);
			}
		};
	});

	// 自动轮播：减少动态效果、悬停、聚焦、页面不可见时都暂停（WCAG 2.2.2）
	$effect(() => {
		if (paused || reduceMotion || slides.length < 2) {
			return;
		}
		const timer = setInterval(() => go(active + 1), INTERVAL_MS);
		return () => clearInterval(timer);
	});

	let scrollTimer: ReturnType<typeof setTimeout> | undefined;
	function onScroll() {
		clearTimeout(scrollTimer);
		scrollTimer = setTimeout(sync, 80);
	}
</script>

<svelte:head>
	{#if lead}
		<!--
		  首图预加载：<svelte:head> 排在内联样式前面，HTML 的第一个包就能让浏览器发出请求，
		  不用等解析到 body 里的 <picture>。media 与下面的 <source> 互补，同一时刻只有一条生效；
		  srcset / sizes 与 <img> 完全一致，浏览器挑出同一个地址，不会重复下载。
		-->
		<link
			rel="preload"
			as="image"
			fetchpriority="high"
			media="not all and {LEAD_DESKTOP_MEDIA}"
			href={lead.mobileSrcset ? undefined : lead.mobile}
			imagesrcset={lead.mobileSrcset || undefined}
			imagesizes={lead.mobileSrcset ? LEAD_MOBILE_SIZES : undefined}
		/>
		<link
			rel="preload"
			as="image"
			fetchpriority="high"
			media={LEAD_DESKTOP_MEDIA}
			href={lead.desktopSrcset ? undefined : lead.desktop}
			imagesrcset={lead.desktopSrcset || undefined}
			imagesizes={lead.desktopSrcset ? LEAD_DESKTOP_SIZES : undefined}
		/>
	{/if}
</svelte:head>

<!--
  首页轮播。手机是竖版整屏大图 + 居中标题 + 一个大「立即观看」（图五那种）；
  电脑是满幅横幅 + 左下文案 + 右下缩略图切换（图四那种），缩略图替代圆点，一眼能看到后面几部。
-->
<section
	class="relative"
	aria-roledescription="carousel"
	aria-label={label}
	onmouseenter={() => (paused = true)}
	onmouseleave={() => (paused = false)}
	onfocusin={() => (paused = true)}
	onfocusout={() => (paused = false)}
>
	<ul
		bind:this={track}
		onscroll={onScroll}
		class="no-scrollbar relative -mx-4 flex snap-x snap-mandatory overflow-x-auto md:-mx-6 lg:mx-0 lg:overflow-hidden lg:rounded-[1.25rem]"
	>
		{#each slides as drama, index (drama.id)}
			{@const rating = score(drama.score)}
			<li
				class="relative w-full shrink-0 snap-center overflow-hidden bg-sunken"
				aria-roledescription="slide"
				aria-label={i18n.t('home.slideOf', { n: index + 1, total: slides.length })}
				aria-hidden={index !== active ? 'true' : undefined}
				inert={index !== active && slides.length > 1 ? true : undefined}
			>
				<!-- 电脑端 16:5 并限高 26rem：接近通栏横幅的比例，文案在左下，右下留给缩略图条 -->
				<div class="relative aspect-[4/5] sm:aspect-[16/9] lg:aspect-[16/5] lg:max-h-[26rem] lg:w-full">
					<!-- 手机竖屏用竖封面整幅铺（人脸在上半部，取顶部对齐），电脑优先横封面 -->
					{#if index === 0 && lead}
						<!-- 首图：底下垫渐变占位，<picture> 直接盖在上面。不做淡入——淡入要等脚本，LCP 也会跟着推迟 -->
						<div class="absolute inset-0 overflow-hidden" aria-hidden="true">
							<Poster cover="" title={drama.title} shape="landscape" decorative class="absolute! inset-0 size-full" />
							{#if leadStage !== 'failed'}
								{#key leadStage}
									<picture>
										<source
											media={LEAD_DESKTOP_MEDIA}
											srcset={leadResized && lead.desktopSrcset ? lead.desktopSrcset : lead.desktop}
											sizes={leadResized && lead.desktopSrcset ? LEAD_DESKTOP_SIZES : undefined}
										/>
										<!-- data-lead：src/hooks.client.ts 等它画出来再水合 -->
										<img
											bind:this={leadImg}
											data-lead=""
											src={leadResized ? lead.mobileSrc : lead.mobile}
											srcset={leadResized ? lead.mobileSrcset : undefined}
											sizes={leadResized ? LEAD_MOBILE_SIZES : undefined}
											alt=""
											fetchpriority="high"
											class="absolute inset-0 size-full object-cover object-top"
											onload={onLeadLoad}
											onerror={onLeadError}
										/>
									</picture>
								{/key}
							{/if}
						</div>
					{:else}
						<div class="absolute inset-0 overflow-hidden lg:hidden" aria-hidden="true">
							<Poster
								cover={drama.cover_portrait || drama.cover_landscape}
								title={drama.title}
								shape="landscape"
								decorative
								class="absolute! inset-0 size-full"
								imgClass="object-cover object-top"
							/>
						</div>
						<div class="absolute inset-0 hidden overflow-hidden lg:block" aria-hidden="true">
							<Poster
								cover={drama.cover_landscape || drama.cover_portrait}
								title={drama.title}
								shape="landscape"
								decorative
								class="absolute! inset-0 size-full"
								imgClass="object-cover object-top"
							/>
						</div>
					{/if}
					<!-- 文字一侧压暗，保证白字在任何封面上可读 -->
					<div
						class="absolute inset-0 bg-gradient-to-t from-black/90 via-black/35 to-black/10 lg:bg-gradient-to-r lg:from-black/85 lg:via-black/45 lg:to-transparent rtl:lg:bg-gradient-to-l"
					></div>

					<div
						class="absolute inset-x-0 bottom-0 flex flex-col items-center gap-3 px-5 pb-14 text-center text-white sm:pb-16 lg:inset-y-auto lg:end-auto lg:bottom-0 lg:w-[52%] lg:items-start lg:gap-4 lg:px-14 lg:pb-14 lg:text-start"
					>
						<Badge value={drama.badge} size="lg" />
						<h3
							class="line-clamp-2 text-[1.6rem] leading-tight font-extrabold tracking-tight text-balance drop-shadow-[0_2px_12px_rgb(0_0_0/0.5)] sm:text-3xl lg:text-[2.3rem] xl:text-[2.75rem]"
						>
							{drama.title}
						</h3>

						<p class="flex flex-wrap items-center justify-center gap-x-3 gap-y-1 text-xs text-white/85 lg:justify-start lg:text-sm">
							{#if rating}
								<span class="flex items-center gap-1 font-bold text-[#f7cd6a]">
									<Icon name="star" size={14} filled strokeWidth={0} />{rating}
								</span>
							{/if}
							<span>{episodesLabel(i18n, drama)}</span>
							{#if drama.year}<span>{drama.year}</span>{/if}
							{#if drama.play_count > 0}
								<span class="flex items-center gap-1">
									<Icon name="flame" size={14} />{i18n.t('drama.plays', {
										n: compactNumber(drama.play_count, i18n.locale)
									})}
								</span>
							{/if}
						</p>

						<div class="mt-1 flex w-full items-center justify-center gap-3 lg:mt-2 lg:w-auto lg:justify-start">
							{#if drama.latest_episode > 0}
								<a
									href={i18n.path(playPath(drama.id, 1))}
									class="btn btn-lg btn-light w-full max-w-[17rem] gap-2 lg:w-auto"
									tabindex={index === active ? 0 : -1}
								>
									<Icon name="play" size={19} filled strokeWidth={0} />
									{i18n.t('drama.playNow')}
								</a>
							{:else}
								<a
									href={i18n.path(dramaPath(drama.id))}
									class="btn btn-lg btn-light w-full max-w-[17rem] gap-2 lg:w-auto"
									tabindex={index === active ? 0 : -1}
								>
									<Icon name="info" size={18} />
									{i18n.t('drama.details')}
								</a>
							{/if}
							{#if drama.latest_episode > 0}
								<a
									href={i18n.path(dramaPath(drama.id))}
									class="btn btn-lg btn-glass hidden gap-2 lg:inline-flex"
									tabindex={index === active ? 0 : -1}
								>
									<Icon name="info" size={18} />
									{i18n.t('drama.details')}
								</a>
							{/if}
						</div>
					</div>

					<!-- 图片区域整块可点进详情（按钮在上层，不会被它吃掉点击） -->
					<a
						href={i18n.path(dramaPath(drama.id))}
						class="absolute inset-x-0 top-0 bottom-[42%] lg:bottom-0 lg:start-[52%]"
						aria-label={drama.title}
						tabindex="-1"
					></a>
				</div>
			</li>
		{/each}
	</ul>

	{#if slides.length > 1}
		<!-- 手机：图内底部圆点（图五那种，当前一条拉长）。
		     按钮本身撑到 24px 高（WCAG 2.5.8 的最小目标），里面才是那条 6px 的小圆点 -->
		<div class="absolute inset-x-0 bottom-2.5 flex items-center justify-center sm:bottom-3.5 lg:hidden">
			{#each slides as drama, index (drama.id)}
				<button
					type="button"
					class="grid size-6 place-items-center"
					aria-label={i18n.t('home.goToSlide', { n: index + 1 })}
					aria-current={index === active ? 'true' : undefined}
					onclick={() => go(index)}
				>
					<span
						class="block h-1.5 rounded-full transition-all duration-300 {index === active ? 'w-5 bg-white' : 'w-1.5 bg-white/45'}"
					></span>
				</button>
			{/each}
		</div>

		<!-- 电脑：右下缩略图条。空闲后才挂载，首屏不为这几张小图付带宽 -->
		{#if showThumbs}
			<div class="absolute end-6 bottom-6 hidden items-end gap-2 xl:end-10 lg:flex">
				{#each slides as drama, index (drama.id)}
					<button
						type="button"
						class="relative block shrink-0 overflow-hidden rounded-lg transition-all duration-300 {index === active
							? 'w-[3.75rem] ring-2 ring-white'
							: 'w-13 opacity-65 ring-1 ring-white/30 hover:opacity-100'}"
						aria-label={i18n.t('home.goToSlide', { n: index + 1 })}
						aria-current={index === active ? 'true' : undefined}
						onclick={() => go(index)}
					>
						<Poster cover={drama.cover_portrait || drama.cover_landscape} title={drama.title} decorative />
					</button>
				{/each}
			</div>
		{/if}

		<button
			type="button"
			class="btn-glass absolute start-4 top-1/2 hidden size-11 -translate-y-1/2 place-items-center rounded-full lg:grid"
			aria-label={i18n.t('common.prev')}
			onclick={() => go(active - 1)}
		>
			<Icon name="chevron-left" size={22} class="rtl:rotate-180" />
		</button>
		<button
			type="button"
			class="btn-glass absolute end-4 top-1/2 hidden size-11 -translate-y-1/2 place-items-center rounded-full lg:grid"
			aria-label={i18n.t('common.next')}
			onclick={() => go(active + 1)}
		>
			<Icon name="chevron-right" size={22} class="rtl:rotate-180" />
		</button>
	{/if}
</section>
