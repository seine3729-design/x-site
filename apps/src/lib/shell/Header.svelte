<script lang="ts">
	import { page } from '$app/state';
	import { getI18n } from '#lib/i18n/context.ts';
	import LanguageMenu from '#lib/i18n/LanguageMenu.svelte';
	import { splitLocale } from '#lib/i18n/locales.ts';
	import { DEFAULT_TOP, isActive } from '#lib/nav.ts';
	import { featureOn, getSite, toNavItems } from '#lib/site.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Logo from './Logo.svelte';
	import ThemeToggle from './ThemeToggle.svelte';
	import UserEntry from './UserEntry.svelte';

	type Props = { hiddenOnMobile?: boolean };
	let { hiddenOnMobile = false }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());
	const path = $derived(splitLocale(page.url.pathname).path);

	// 页头样式由后台配（layout.header_style）：classic 左 Logo 右导航、centered Logo 居中、pill 悬浮胶囊
	const centered = $derived(site.layout.headerStyle === 'centered');
	const pill = $derived(site.layout.headerStyle === 'pill');

	// 后台配了「顶部导航」就用后台的，没配用默认五项；排行开关关闭时去掉排行
	const items = $derived.by(() => {
		const configured = toNavItems(site.menus, 'top').filter(
			(item) =>
				item.device !== 'mobile' &&
				(item.visibleTo === 'all' || (item.visibleTo === 'auth') === session.loggedIn)
		);
		if (configured.length > 0) {
			return configured.map((item) => ({ ...item, label: item.label }));
		}
		return DEFAULT_TOP.filter((item) => item.key !== 'rank' || featureOn(site.features, 'ranks')).map((item) => ({
			key: item.key,
			label: i18n.t(item.labelKey),
			href: item.href,
			external: false,
			newTab: false,
			badge: ''
		}));
	});

	let scrolled = $state(false);

	$effect(() => {
		const onScroll = () => {
			scrolled = window.scrollY > 8;
		};
		onScroll();
		window.addEventListener('scroll', onScroll, { passive: true });
		return () => window.removeEventListener('scroll', onScroll);
	});

	const query = $derived(path === '/search' ? (page.url.searchParams.get('q') ?? '') : '');
</script>

{#snippet bar()}
	<Logo class={centered ? 'lg:order-2 lg:justify-self-center' : ''} />

	<nav class="hidden h-full min-w-0 items-center gap-1 lg:flex {centered ? 'lg:order-1' : ''}" aria-label={i18n.t('nav.primary')}>
		{#each items as item (item.key)}
			{@const active = !item.external && isActive(item.href, path)}
			<a
				href={item.external ? item.href : i18n.path(item.href)}
				target={item.newTab ? '_blank' : undefined}
				rel={item.external ? 'noopener noreferrer' : undefined}
				aria-current={active ? 'page' : undefined}
				class="relative flex h-full items-center px-3 text-[15px] font-medium text-fg-muted transition-colors hover:text-fg aria-[current=page]:font-semibold aria-[current=page]:text-fg"
			>
				{item.label}
				{#if item.badge}
					<span class="ms-1 rounded-full bg-brand px-1.5 py-px text-[10px] leading-4 font-bold text-on-brand">{item.badge}</span>
				{/if}
				{#if active}
					<span class="absolute inset-x-3 bottom-0 h-[3px] rounded-full bg-brand" aria-hidden="true"></span>
				{/if}
			</a>
		{/each}
	</nav>

	<div class="ms-auto flex min-w-0 items-center gap-2 {centered ? 'lg:order-3 lg:justify-self-end' : ''}">
		<!-- 电脑端搜索框：不依赖 JS 的 GET 表单 -->
		<!-- 空间不够时搜索框先变窄，不挤掉右侧按钮，也不撑破页头 -->
		<form action={i18n.path('/search')} method="get" role="search" class="relative hidden w-full min-w-0 max-w-56 md:block xl:max-w-72">
			<label class="sr-only" for="header-search">{i18n.t('search.placeholder')}</label>
			<Icon name="search" size={18} class="pointer-events-none absolute start-3.5 top-1/2 -translate-y-1/2 text-fg-subtle" />
			<input
				id="header-search"
				name="q"
				type="search"
				value={query}
				autocomplete="off"
				enterkeyhint="search"
				maxlength="50"
				placeholder={i18n.t('search.placeholder')}
				class="h-10 w-full rounded-full border border-line bg-raised/70 ps-10 pe-4 text-sm text-fg placeholder:text-fg-subtle focus:border-line-strong focus:bg-raised focus:outline-none"
			/>
		</form>
		<a href={i18n.path('/search')} class="icon-btn md:hidden" aria-label={i18n.t('search.title')}>
			<Icon name="search" size={21} />
		</a>

		<div class="hidden items-center gap-1 lg:flex">
			<LanguageMenu compact />
			<ThemeToggle />
		</div>

		<a href={i18n.path('/vip')} class="btn btn-vip btn-sm hidden gap-1.5 font-bold lg:inline-flex">
			<Icon name="crown" size={16} />
			{i18n.t('vip.cta')}
		</a>

		<UserEntry />
	</div>
{/snippet}

<header
	class="sticky top-0 z-40 h-(--header-h) transition-[background-color,border-color] duration-200 {pill
		? ''
		: scrolled
			? 'border-b border-line bg-canvas/85 backdrop-blur-xl'
			: 'border-b border-transparent bg-canvas/60 backdrop-blur-md'} {hiddenOnMobile ? 'max-lg:hidden' : ''}"
>
	{#if pill}
		<!-- 胶囊：页头本身透明，内容装在一条悬浮的圆角条里 -->
		<div class="page-x flex h-full items-center py-1.5">
			<div
				class="flex h-full w-full items-center gap-2 rounded-full border border-line bg-surface/80 px-3 shadow-pop backdrop-blur-xl lg:gap-6 lg:px-5"
			>
				{@render bar()}
			</div>
		</div>
	{:else if centered}
		<!-- 两侧用 minmax(0,1fr)：1fr 不能缩到 min-content 以下，语言较长时（阿拉伯语）页头会撑出横向滚动 -->
		<div class="page-x grid h-full grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 lg:gap-8">
			<!-- 手机上让 Logo 落到中间那一格（电脑上这一格由导航占） -->
			<div class="lg:hidden" aria-hidden="true"></div>
			{@render bar()}
		</div>
	{:else}
		<div class="page-x flex h-full items-center gap-2 lg:gap-8">
			{@render bar()}
		</div>
	{/if}
</header>
