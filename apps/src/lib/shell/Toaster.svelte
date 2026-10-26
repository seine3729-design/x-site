<script lang="ts">
	import { toasts } from '#lib/stores/toast.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';

	const ICON = { info: 'info', success: 'circle-check', error: 'triangle-alert' } as const;
</script>

<!-- 读屏以 polite 方式播报，不抢焦点 -->
<div
	class="pointer-events-none fixed inset-x-0 top-[calc(var(--header-h)+0.75rem)] z-[90] flex flex-col items-center gap-2 px-4"
	role="status"
	aria-live="polite"
>
	{#each toasts.items as toast (toast.id)}
		<div
			class="pointer-events-auto flex max-w-md animate-rise items-center gap-3 rounded-panel border border-line bg-overlay/95 px-4 py-3 text-sm shadow-pop backdrop-blur"
		>
			<Icon
				name={ICON[toast.tone]}
				size={18}
				class={toast.tone === 'error' ? 'text-danger' : toast.tone === 'success' ? 'text-ok' : 'text-brand-fg'}
			/>
			<span class="text-fg">{toast.text}</span>
			{#if toast.action}
				<button
					type="button"
					class="ms-2 font-semibold text-brand-fg hover:underline"
					onclick={() => {
						toast.action?.run();
						toasts.dismiss(toast.id);
					}}
				>
					{toast.action.label}
				</button>
			{/if}
		</div>
	{/each}
</div>
