<script lang="ts">
	import AdSlot from '#lib/home/AdSlot.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import LanguageMenu from '#lib/i18n/LanguageMenu.svelte';
	import { getSite, toNavItems } from '#lib/site.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Logo from './Logo.svelte';

	const i18n = getI18n();
	const site = $derived(getSite());
	const year = new Date().getFullYear();

	// 后台「页脚」菜单按分组标题归组；「帮助与条款」来自已发布的页脚静态页
	const groups = $derived.by(() => {
		const map: Record<string, ReturnType<typeof toNavItems>> = {};
		for (const item of toNavItems(site.menus, 'footer')) {
			const title = item.group || i18n.t('footer.more');
			(map[title] ??= []).push(item);
		}
		return Object.entries(map);
	});
</script>

<footer class="mt-16 border-t border-line bg-surface/40 pb-[calc(var(--tabbar-h)+var(--safe-bottom)+1.5rem)] lg:pb-10">
	<!-- 移动端：品牌区独占一行，导航分组两列并排并收紧行距，避免单列堆叠显得空旷；md 以上维持原来的四列布局 -->
	<div class="page-x grid grid-cols-2 gap-x-6 gap-y-8 pt-10 md:grid-cols-[1.4fr_repeat(3,1fr)] md:gap-10">
		<div class="col-span-2 flex flex-col gap-4 md:col-span-1">
			<Logo />
			{#if site.slogan}
				<p class="max-w-xs text-sm leading-6 text-fg-muted">{site.slogan}</p>
			{/if}
			<div class="flex flex-wrap items-center gap-2">
				<LanguageMenu placement="up" />
				<a href={i18n.path('/way-home')} class="chip h-10 gap-1.5 px-4">
					<Icon name="map-pin" size={15} />
					{i18n.t('footer.wayHome')}
				</a>
			</div>
		</div>

		<nav aria-label={i18n.t('footer.browse')}>
			<h2 class="mb-3 text-sm font-semibold text-fg">{i18n.t('footer.browse')}</h2>
			<ul class="space-y-1 text-sm text-fg-muted">
				<li><a class="inline-block py-1 hover:text-fg" href={i18n.path('/explore')}>{i18n.t('nav.explore')}</a></li>
				<li><a class="inline-block py-1 hover:text-fg" href={i18n.path('/rank')}>{i18n.t('nav.rank')}</a></li>
				<li><a class="inline-block py-1 hover:text-fg" href={i18n.path('/topics')}>{i18n.t('nav.topics')}</a></li>
				<li><a class="inline-block py-1 hover:text-fg" href={i18n.path('/shua')}>{i18n.t('nav.shua')}</a></li>
			</ul>
		</nav>

		{#if site.footerPages.length > 0}
			<nav aria-label={i18n.t('footer.help')}>
				<h2 class="mb-3 text-sm font-semibold text-fg">{i18n.t('footer.help')}</h2>
				<ul class="space-y-1 text-sm text-fg-muted">
					{#each site.footerPages as item (item.slug)}
						<li><a class="inline-block py-1 hover:text-fg" href={i18n.path(`/p/${encodeURIComponent(item.slug)}`)}>{item.title}</a></li>
					{/each}
				</ul>
			</nav>
		{/if}

		{#each groups as [title, links] (title)}
			<nav aria-label={title}>
				<h2 class="mb-3 text-sm font-semibold text-fg">{title}</h2>
				<ul class="space-y-1 text-sm text-fg-muted">
					{#each links as link (link.key)}
						<li>
							<a
								class="inline-flex items-center gap-1 py-1 hover:text-fg"
								href={link.external ? link.href : i18n.path(link.href)}
								target={link.newTab ? '_blank' : undefined}
								rel={link.external ? 'noopener noreferrer' : undefined}
							>
								{link.label}
								{#if link.external}<Icon name="external-link" size={13} />{/if}
							</a>
						</li>
					{/each}
				</ul>
			</nav>
		{/each}
	</div>
	<!-- 页脚横幅与文字链广告位（docs/04 C.5） -->
	<div class="page-x mt-8 flex flex-col gap-3">
		<AdSlot slot="footer_banner" />
		<AdSlot slot="text_link" />
	</div>
	<div class="page-x mt-8 border-t border-line pt-6 text-xs leading-5 text-fg-subtle md:mt-10">
		<!-- 后台填了版权文字就用它（已按语言自行填写），否则用文案模板 -->
		<p>{site.copyright || i18n.t('footer.copyright', { year, name: site.name })}</p>
	</div>
</footer>
