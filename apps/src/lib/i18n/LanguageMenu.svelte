<script lang="ts">
	import { page } from '$app/state';
	import Icon from '#lib/ui/Icon.svelte';
	import { getI18n } from './context.ts';
	import { LOCALES, LOCALE_NAMES, localizePath, splitLocale, type Locale } from './locales.ts';
	import { rememberLocale } from './preference.ts';

	type Props = { compact?: boolean; placement?: 'down' | 'up' };
	let { compact = false, placement = 'down' }: Props = $props();

	const i18n = getI18n();

	// 当前页面在其他语言下的地址（保留查询串）
	const hrefFor = (locale: Locale) => localizePath(splitLocale(page.url.pathname).path, locale) + page.url.search;
</script>

<!-- 不依赖 JS：details/summary 展开，链接整页跳转（让 <html lang/dir> 随语言更新） -->
<details class="group relative">
	<summary
		class="flex h-10 list-none items-center gap-1.5 rounded-full px-3 text-sm text-fg-muted transition-colors hover:bg-raised hover:text-fg [&::-webkit-details-marker]:hidden"
		aria-label={i18n.t('lang.label')}
	>
		<Icon name="globe" size={18} />
		{#if !compact}
			<span>{LOCALE_NAMES[i18n.locale]}</span>
		{/if}
		<Icon name="chevron-down" size={14} class="transition-transform group-open:rotate-180" />
	</summary>
	<ul
		class="absolute end-0 z-50 min-w-40 animate-fade-in rounded-xl border border-line bg-overlay p-1.5 text-sm shadow-pop {placement ===
		'up'
			? 'bottom-full mb-2'
			: 'mt-2'}"
	>
		{#each LOCALES as locale (locale)}
			<li>
				<!-- 语言地址由 localizePath 生成，整页跳转 -->
				<a
					href={hrefFor(locale)}
					hreflang={locale}
					lang={locale}
					data-sveltekit-reload
					aria-current={locale === i18n.locale ? 'true' : undefined}
					class="flex items-center justify-between gap-3 rounded-lg px-3 py-2 text-fg-muted hover:bg-raised hover:text-fg aria-[current=true]:font-semibold aria-[current=true]:text-fg"
					onclick={() => rememberLocale(locale)}
				>
					{LOCALE_NAMES[locale]}
					{#if locale === i18n.locale}
						<Icon name="check" size={16} class="text-brand-fg" />
					{/if}
				</a>
			</li>
		{/each}
	</ul>
</details>
