<script lang="ts">
	import type { Snippet } from 'svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = { title: string; actions?: Snippet };
	let { title, actions }: Props = $props();
	const i18n = getI18n();
</script>

<div class="mb-5 flex items-center gap-2 lg:mb-7">
	<a href={i18n.path('/me')} class="icon-btn -ms-2" aria-label={i18n.t('nav.back')}>
		<Icon name="chevron-left" size={24} class="rtl:rotate-180" />
	</a>
	<h1 class="min-w-0 flex-1 truncate text-xl font-extrabold tracking-tight lg:text-2xl">{title}</h1>
	{#if actions}<div class="flex items-center gap-2">{@render actions()}</div>{/if}
</div>
