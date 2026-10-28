<script lang="ts">
	import { afterNavigate } from '$app/navigation';
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
	});

	afterNavigate(({ type, to }) => {
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
