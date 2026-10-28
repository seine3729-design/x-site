<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import BlobImage from '#lib/media/BlobImage.svelte';
	import { imageSources } from '#lib/media/image.ts';
	import { getSite } from '#lib/site.ts';

	type Props = { class?: string; showName?: boolean };
	let { class: className = '', showName = true }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());
	const logo = $derived(imageSources(site.logo, site.imageBases));
</script>

{#snippet mark()}
	<span class="brand-mark grid size-8 place-items-center rounded-[10px]" aria-hidden="true">
		<svg viewBox="0 0 24 24" width="16" height="16" class="translate-x-px fill-white"
			><path d="M7 4.8v14.4a1 1 0 0 0 1.5.86l11.6-7.2a1 1 0 0 0 0-1.72L8.5 3.94A1 1 0 0 0 7 4.8Z" /></svg
		>
	</span>
{/snippet}

<a href={i18n.path('/')} class="flex shrink-0 items-center gap-2.5 {className}" aria-label={site.name}>
	{#if logo.length > 0}
		<BlobImage
			src={logo}
			alt=""
			height={32}
			priority
			class="h-8 w-auto"
			fallback={mark}
		/>
	{:else}
		{@render mark()}
	{/if}
</a>
