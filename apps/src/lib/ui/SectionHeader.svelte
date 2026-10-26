<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from './Icon.svelte';

	type Props = { title: string; subtitle?: string; more?: string; id?: string; level?: 'h2' | 'h3' };
	let { title, subtitle = '', more = '', id, level = 'h2' }: Props = $props();

	const i18n = getI18n();
	const href = $derived.by(() => {
		const value = more.trim();
		if (/^https?:\/\//i.test(value)) {
			return { href: value, external: true };
		}
		return value.startsWith('/') && !value.startsWith('//') ? { href: i18n.path(value), external: false } : null;
	});
</script>

<div class="mb-3 flex items-end justify-between gap-4 md:mb-4">
	<div class="min-w-0">
		<svelte:element this={level} {id} class="section-title flex items-center gap-2">
			<span class="h-[1.05em] w-1 shrink-0 rounded-full bg-brand" aria-hidden="true"></span>
			<span class="truncate">{title}</span>
		</svelte:element>
		{#if subtitle}
			<p class="mt-1 truncate ps-3 text-[13px] text-fg-subtle">{subtitle}</p>
		{/if}
	</div>
	{#if href}
		<a
			href={href.href}
			target={href.external ? '_blank' : undefined}
			rel={href.external ? 'noopener noreferrer' : undefined}
			class="flex shrink-0 items-center gap-0.5 text-[13px] font-medium text-fg-subtle transition-colors hover:text-fg"
		>
			{i18n.t('common.more')}
			<Icon name="chevron-right" size={16} class="rtl:rotate-180" />
		</a>
	{/if}
</div>
