<script lang="ts">
	import { afterNavigate, onNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { PUBLIC_SITE_URL } from '$app/env/public';
	import { onMount } from 'svelte';
	// no-inline 让 Vite 始终输出成文件：内联成 data: 地址会被生产环境的 img-src 拦掉
	import favicon from '#lib/assets/favicon.svg?no-inline';
	import { setI18n } from '#lib/i18n/context.ts';
	import { DEFAULT_LOCALE, LOCALES, localizePath, splitLocale } from '#lib/i18n/locales.ts';
	import { suggestedRedirect } from '#lib/i18n/preference.ts';
	import { imageSources } from '#lib/media/image.ts';
	import { immersiveRoute } from '#lib/nav.ts';
	import RegisterServiceWorker from '#lib/pwa/register-service-worker.svelte';
	import AdFloat from '#lib/shell/AdFloat.svelte';
	import AdPopup from '#lib/shell/AdPopup.svelte';
	import AgeGate from '#lib/shell/AgeGate.svelte';
	import Announcements from '#lib/shell/Announcements.svelte';
	import BackToTop from '#lib/shell/BackToTop.svelte';
	import Footer from '#lib/shell/Footer.svelte';
	import Header from '#lib/shell/Header.svelte';
	import LegacyNotice from '#lib/shell/LegacyNotice.svelte';
	import TabBar from '#lib/shell/TabBar.svelte';
	import Toaster from '#lib/shell/Toaster.svelte';
	import { setSite } from '#lib/site.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import '../app.css';

	let { data, children } = $props();

	const i18n = setI18n(() => data.i18n);
	setSite(() => data.site);

	// hreflang：告诉搜索引擎同一页面的各语言版本
	const origin = $derived(PUBLIC_SITE_URL || page.url.origin);
	const barePath = $derived(splitLocale(page.url.pathname).path);
	const chrome = $derived(immersiveRoute(page.route.id));
	// favicon 由后台配（留空用内置图标）；爬虫与系统不执行 JS，这里用普通地址而不是 blob:（docs/04 C.8）
	const icon = $derived(imageSources(data.site.favicon, data.site.imageBases)[0] ?? favicon);

	/** 首访按浏览器语言跳到对应语言版本时，这一页不记埋点（跳过去之后再记） */
	let redirecting = false;

	onMount(() => {
		const target = suggestedRedirect(location, navigator.languages, navigator.userAgent);
		if (target) {
			redirecting = true;
			location.replace(target);
			return;
		}
		void import('#lib/api/client.ts').then(({ setApiLocale }) => setApiLocale(i18n.locale));
		void session.refresh();

		/*
		 * 进度条从「点下站内链接」就开始计时，而不是等 onNavigate。
		 * SvelteKit 的 preload-data 在触摸按下时就去取数据了，真正的等待发生在 onNavigate **之前**：
		 * 实测把 `__data.json` 延迟 600ms，onNavigate 直到 600ms 才进入，之后到落地只剩 85ms，
		 * 180ms 的阈值还没到就被 afterNavigate 清掉——进度条挂在 onNavigate 上等于永远不会出现。
		 */
		const onLinkClick = (event: MouseEvent) => {
			if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
				return;
			}
			const link = (event.target as Element | null)?.closest?.('a[href]');
			if (!(link instanceof HTMLAnchorElement)) {
				return;
			}
			// 只管会走客户端导航的站内链接；新窗口、下载、外站、点的就是当前页都不摆进度条
			if (
				link.target === '_blank' ||
				link.hasAttribute('download') ||
				link.origin !== location.origin ||
				link.href === location.href
			) {
				return;
			}
			startNavHint();
		};
		document.addEventListener('click', onLinkClick, { capture: true });
		return () => document.removeEventListener('click', onLinkClick, { capture: true });
	});

	/**
	 * 导航进度条。客户端导航要等一次服务端往返（线上实测详情页 `__data.json` 的 ttfb 约 310ms、
	 * 从按下到出内容约 616ms），而这段时间界面原来一点变化都没有，用户只会觉得「点了没反应」。
	 * `__data.json` 被 SvelteKit 固定成 `private, no-store`，拿不到边缘缓存，这个往返省不掉，
	 * 所以这里做的是「把等待说清楚」，不是把等待变短。
	 *
	 * 延迟 `NAV_HINT_MS` 才显示：命中预加载缓存的导航几十毫秒就完成，不该为它闪一下进度条
	 * （与骨架屏、播放器转圈同一条原则，见 C.8 与 C.1）。
	 */
	const NAV_HINT_MS = 180;
	/** 兜底：SvelteKit 判定这次点击不需要导航（或导航被打断）时，afterNavigate 不会来，得自己收掉 */
	const NAV_HINT_MAX_MS = 8000;
	let navPending = $state(false);
	let navTimer: ReturnType<typeof setTimeout> | undefined;
	let navGiveUp: ReturnType<typeof setTimeout> | undefined;

	function startNavHint() {
		clearTimeout(navTimer);
		clearTimeout(navGiveUp);
		navTimer = setTimeout(() => (navPending = true), NAV_HINT_MS);
		navGiveUp = setTimeout(endNavHint, NAV_HINT_MAX_MS);
	}

	function endNavHint() {
		clearTimeout(navTimer);
		clearTimeout(navGiveUp);
		navPending = false;
	}

	afterNavigate(({ type, to }) => {
		endNavHint();
		// 客户端导航后把焦点交给主内容，读屏用户能听到新页面（WCAG 2.4.3）
		if (type !== 'enter') {
			document.getElementById('main')?.focus({ preventScroll: true });
		}
		// 埋点（W4-1，docs/04 C.10）：来访、浏览与 Web Vitals。埋点代码动态导入，不进首屏包。
		// 路由模板取 page.route.id：首屏（enter）时 to.route.id 是 null，实测会让首屏的浏览丢掉路由
		if (to && !redirecting) {
			const route = page.route.id ?? to.route.id;
			const { url } = to;
			const { locale } = i18n;
			void import('#lib/analytics/runtime.ts').then((analytics) => analytics.navigated(type, url, route, locale));
		}
	});

	onNavigate((navigation) => {
		// 整页跳走（不是客户端导航）时什么都不做：页面马上就要被替换掉了
		if (navigation.willUnload) {
			return;
		}
		// 代码触发的导航（goto 等）走不到下面的链接点击监听，这里补一次计时
		startNavHint();

		/*
		 * 页面切换动画，用 View Transitions API。
		 *
		 * 三个前提缺一个就直接跳过，退回原来的「瞬间替换」（渐进增强）：
		 *   - 浏览器支持（Chrome 111+ / Safari 18+；本站 JS 基线是 Chrome 100，所以必须特性检测）；
		 *   - 用户没有要求减少动效；
		 *   - 这次导航换了路由模板。同一个路由模板内部的跳转不做整页过渡——播放页换集走的是
		 *     `goto(..., { replace: true })`，整屏淡入淡出会让播放器画面明显闪一下。
		 *
		 * 注意它**不能**让等待变短：startViewTransition 的回调里要 await navigation.complete，
		 * 动画是在数据就绪之后才跑的。等待期间靠上面那条进度条交代。
		 */
		if (navigation.from?.route.id === navigation.to?.route.id) {
			return;
		}
		if (typeof document.startViewTransition !== 'function') {
			return;
		}
		if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
			return;
		}
		return new Promise<void>((resolve) => {
			document.startViewTransition(async () => {
				resolve();
				await navigation.complete;
			});
		});
	});
</script>

<svelte:head>
	<link rel="icon" href={icon} />
	<!--
	  这里刻意不再写 preconnect：CDN 的 CORS 连接由 head 最前面那批 modulepreload 立刻建起来，
	  图片缩放走同源；原来的两条（CDN 前缀、第一条图片线路）都指向同一个域名，
	  Lighthouse 判定为「未使用的预连接」，而且连同 hooks 补的那条一共三条，超出了建议的数量。
	-->
	{#each LOCALES as locale (locale)}
		<link rel="alternate" hreflang={locale} href={origin + localizePath(barePath, locale)} />
	{/each}
	<link rel="alternate" hreflang="x-default" href={origin + localizePath(barePath, DEFAULT_LOCALE)} />
</svelte:head>

<a
	href="#main"
	class="sr-only z-[110] rounded-full bg-brand px-4 py-2 text-sm font-semibold text-on-brand focus:not-sr-only focus:fixed focus:start-4 focus:top-3"
>
	{i18n.t('nav.skip')}
</a>

<!--
  导航进度条：只在客户端导航超过 NAV_HINT_MS 还没完成时出现。
  对读屏隐藏——导航结束后焦点会移到 #main，读屏那时自然会念新页面，不需要再播报一次进度。
-->
{#if navPending}
	<div class="nav-progress" aria-hidden="true"></div>
{/if}

<div class="flex min-h-dvh flex-col">
	<Header hiddenOnMobile={!chrome.header} />
	<LegacyNotice />
	<Announcements />

	<!-- 底栏占位由页脚（或沉浸式页面自己）留出，这里不再加内边距 -->
	<main id="main" tabindex="-1" class="flex-1 outline-none">
		{@render children()}
	</main>

	{#if chrome.footer}
		<Footer />
	{:else}
		<div class="hidden lg:block"><Footer /></div>
	{/if}
</div>

<!-- 底栏可被后台关掉（layout.mobile_tabbar）；关掉时注入的 --tabbar-h: 0rem 同时收掉底部留白 -->
{#if chrome.tabbar && data.site.layout.mobileTabbar}
	<TabBar />
{/if}
<BackToTop />
<Toaster />
<AgeGate />
<!-- 全站弹窗与 APP 下载浮窗广告位（docs/04 C.5）：没有素材时都不出现 -->
<AdPopup />
<AdFloat />
<RegisterServiceWorker />

<style>
	/*
	  导航进度条。只动 transform，跑在合成器线程，不触发重排重绘。
	  8 秒的 ease-out 曲线让它前期飞快、后期几乎不动，永远到不了头：导航完成时元素直接被卸载，
	  不必再补一段「跳到 100%」的收尾动画（真实导航 300–600ms，只会用到曲线最前面那一小段）。
	*/
	.nav-progress {
		position: fixed;
		inset-inline: 0;
		top: 0;
		z-index: 50;
		height: 2px;
		background: var(--brand);
		transform-origin: left;
		animation: nav-progress 8s cubic-bezier(0.1, 0.85, 0.2, 1) both;
	}
	/* RTL 下要从右边长出来；transform-origin 没有逻辑属性写法，只能按方向覆盖 */
	:global([dir='rtl']) .nav-progress {
		transform-origin: right;
	}
	@keyframes nav-progress {
		from {
			transform: scaleX(0);
		}
		to {
			transform: scaleX(0.92);
		}
	}
	/* 用户要求减少动效时不跑进度动画，留一条静止的短条表示「在加载」 */
	@media (prefers-reduced-motion: reduce) {
		.nav-progress {
			animation: none;
			transform: scaleX(0.35);
		}
	}

	/*
	  页面切换动画（配合 onNavigate 里的 startViewTransition）。
	  伪元素是文档级的，必须用 :global 穿出组件作用域。
	  旧页淡出比新页淡入短，两段重叠：整体 220ms 就结束，不会让人觉得「又多等了一下」。
	  只动 opacity 与 transform，跑在合成器线程。
	*/
	:global(::view-transition-old(root)) {
		animation: vt-out 140ms var(--ease-out-soft) both;
	}
	:global(::view-transition-new(root)) {
		animation: vt-in 220ms var(--ease-out-soft) both;
	}
	@keyframes vt-out {
		to {
			opacity: 0;
		}
	}
	@keyframes vt-in {
		from {
			opacity: 0;
			transform: translateY(8px);
		}
	}
</style>
