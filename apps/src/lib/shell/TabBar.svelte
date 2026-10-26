<script lang="ts">
	import { page } from '$app/state';
	import { getI18n } from '#lib/i18n/context.ts';
	import { splitLocale } from '#lib/i18n/locales.ts';
	import { DEFAULT_TABBAR, isActive } from '#lib/nav.ts';
	import { featureOn, getSite, toNavItems } from '#lib/site.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import { resolveIcon } from '#lib/ui/icons.ts';

	const i18n = getI18n();
	const site = $derived(getSite());
	const path = $derived(splitLocale(page.url.pathname).path);

	// 后台「手机底栏」最多 5 项；没配时用默认五项
	const items = $derived.by(() => {
		const configured = toNavItems(site.menus, 'mobile_tabbar')
			.filter((item) => item.visibleTo === 'all' || (item.visibleTo === 'auth') === session.loggedIn)
			.slice(0, 5)
			.map((item) => ({ ...item, iconName: resolveIcon(item.icon, 'circle-play') }));
		if (configured.length > 0) {
			return configured;
		}
		return DEFAULT_TABBAR.filter((item) => item.key !== 'rank' || featureOn(site.features, 'ranks')).map((item) => ({
			key: item.key,
			label: i18n.t(item.labelKey),
			href: item.href,
			external: false,
			newTab: false,
			badge: '',
			iconName: item.icon
		}));
	});

	const unread = $derived(session.me?.unread ?? 0);
</script>

<nav
	class="fixed inset-x-0 bottom-0 z-40 border-t border-line bg-canvas/90 pb-(--safe-bottom) backdrop-blur-xl lg:hidden"
	aria-label={i18n.t('nav.tabbar')}
>
	<ul class="mx-auto flex h-(--tabbar-h) max-w-xl items-stretch">
		{#each items as item (item.key)}
			{@const active = !item.external && isActive(item.href, path)}
			<li class="flex-1">
				<a
					href={item.external ? item.href : i18n.path(item.href)}
					target={item.newTab ? '_blank' : undefined}
					rel={item.external ? 'noopener noreferrer' : undefined}
					aria-current={active ? 'page' : undefined}
					data-sveltekit-preload-data="tap"
					class="relative flex h-full flex-col items-center justify-center gap-0.5 text-[11px] font-medium text-fg-subtle transition-colors aria-[current=page]:text-fg"
				>
					<span class="relative">
						<Icon
							name={item.iconName}
							size={23}
							strokeWidth={active ? 2.4 : 1.8}
							class={active ? 'text-brand-fg' : ''}
						/>
						{#if item.href === '/me' && unread > 0}
							<span class="absolute -end-1 -top-0.5 size-2 rounded-full bg-brand" aria-hidden="true"></span>
						{/if}
						{#if item.badge}
							<span
								class="absolute -end-3 -top-1.5 rounded-full bg-brand px-1 text-[9px] leading-[14px] font-bold text-on-brand"
								>{item.badge}</span
							>
						{/if}
					</span>
					<span>{item.label}</span>
				</a>
			</li>
		{/each}
	</ul>
</nav>
