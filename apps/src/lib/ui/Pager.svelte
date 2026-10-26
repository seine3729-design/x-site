<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from './Icon.svelte';

	type Props = { page: number; pages: number; href: (page: number) => string };
	let { page, pages, href }: Props = $props();

	const i18n = getI18n();
	// 当前页前后各两页 + 首尾页，中间用省略号
	const items = $derived.by(() => {
		const out: (number | 'gap')[] = [];
		for (let p = 1; p <= pages; p += 1) {
			if (p === 1 || p === pages || Math.abs(p - page) <= 2) {
				out.push(p);
			} else if (out[out.length - 1] !== 'gap') {
				out.push('gap');
			}
		}
		return out;
	});
</script>

{#if pages > 1}
	<nav class="flex items-center justify-center gap-1.5 py-8" aria-label={i18n.t('common.pagination')}>
		<a
			href={page > 1 ? href(page - 1) : undefined}
			class="icon-btn bg-raised {page <= 1 ? 'pointer-events-none opacity-40' : ''}"
			aria-label={i18n.t('common.prevPage')}
			aria-disabled={page <= 1 ? 'true' : undefined}
		>
			<Icon name="chevron-left" size={18} class="rtl:rotate-180" />
		</a>
		{#each items as item, index (index)}
			{#if item === 'gap'}
				<span class="px-1 text-fg-subtle" aria-hidden="true">…</span>
			{:else}
				<a
					href={href(item)}
					class="grid h-10 min-w-10 place-items-center rounded-full px-3 text-sm font-semibold tabular {item === page
						? 'bg-brand text-on-brand'
						: 'bg-raised text-fg-muted hover:text-fg'}"
					aria-current={item === page ? 'page' : undefined}>{item}</a
				>
			{/if}
		{/each}
		<a
			href={page < pages ? href(page + 1) : undefined}
			class="icon-btn bg-raised {page >= pages ? 'pointer-events-none opacity-40' : ''}"
			aria-label={i18n.t('common.nextPage')}
			aria-disabled={page >= pages ? 'true' : undefined}
		>
			<Icon name="chevron-right" size={18} class="rtl:rotate-180" />
		</a>
	</nav>
{/if}
