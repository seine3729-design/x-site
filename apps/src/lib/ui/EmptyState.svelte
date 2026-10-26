<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';
	import type { IconName } from './icons.ts';

	type Props = {
		icon?: IconName;
		title: string;
		description?: string;
		children?: Snippet;
		compact?: boolean;
		/** 整页只有这一块内容时（错误页）把标题作为页面 h1 */
		heading?: 'h1' | 'p';
	};
	let { icon = 'film', title, description = '', children, compact = false, heading = 'p' }: Props = $props();
</script>

<div class="flex flex-col items-center text-center {compact ? 'py-10' : 'py-16 md:py-24'}">
	<span class="relative mb-5 grid size-16 place-items-center rounded-3xl bg-raised text-fg-subtle">
		<span class="absolute inset-0 rounded-3xl bg-brand-soft blur-xl" aria-hidden="true"></span>
		<Icon name={icon} size={30} class="relative" />
	</span>
	<svelte:element this={heading} class="text-base font-semibold text-fg">{title}</svelte:element>
	{#if description}
		<p class="mt-1.5 max-w-sm text-sm leading-6 text-fg-muted">{description}</p>
	{/if}
	{#if children}
		<div class="mt-6 flex flex-wrap justify-center gap-3">{@render children()}</div>
	{/if}
</div>
