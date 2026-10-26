<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = { tags: { id: number; name: string; slug: string }[] };
	let { tags }: Props = $props();

	const i18n = getI18n();
</script>

<ul class="flex flex-wrap gap-2">
	{#each tags as tag (tag.id)}
		<li>
			<a href={i18n.path(`/tag/${encodeURIComponent(tag.slug)}`)} class="chip h-9 gap-1 px-4 text-sm">
				<Icon name="tag" size={13} class="text-fg-subtle" />
				{tag.name}
			</a>
		</li>
	{/each}
</ul>
