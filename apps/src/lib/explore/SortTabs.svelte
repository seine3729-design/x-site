<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';

	type Option = { value: string; href: string; key: 'sort.latest' | 'sort.hot' | 'sort.recommend' | 'sort.random' };
	type Props = { options: Option[]; current: string; label: string };
	let { options, current, label }: Props = $props();

	const i18n = getI18n();
</script>

<nav class="flex items-center gap-1 rounded-full bg-sunken p-1" aria-label={label}>
	{#each options as option (option.value)}
		<a
			href={option.href}
			aria-current={option.value === current ? 'page' : undefined}
			data-sveltekit-noscroll
			data-sveltekit-keepfocus
			class="rounded-full px-3.5 py-1.5 text-[13px] font-semibold text-fg-muted transition-colors hover:text-fg aria-[current=page]:bg-overlay aria-[current=page]:text-fg aria-[current=page]:shadow-sm"
		>
			{i18n.t(option.key)}
		</a>
	{/each}
</nav>
