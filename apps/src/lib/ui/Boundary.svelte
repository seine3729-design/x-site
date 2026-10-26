<script lang="ts">
	/**
	 * 区块级错误边界（docs/04 B.1）：里面的组件渲染出错时只换成一条可重试的提示，不拖垮整页。
	 * 用在首页区块、广告位这类「单块内容」外面；整页级错误仍由 +error.svelte 处理。
	 */
	import { dev } from '$app/env';
	import type { Snippet } from 'svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = {
		children: Snippet;
		/** 出错时写进控制台的标识（如区块类型），方便对着埋点排查 */
		label?: string;
		/** 失败提示不占位、直接隐藏（如广告位出错不该留一块空白） */
		silent?: boolean;
	};
	let { children, label = '', silent = false }: Props = $props();

	const i18n = getI18n();

	/** 开发提示用：抛出来的不一定是 Error（Svelte 会包一层），直接 String() 会得到 [object Object] */
	function describe(error: unknown): string {
		if (error instanceof Error) {
			return error.message;
		}
		if (error && typeof error === 'object' && 'message' in error) {
			return String((error as { message: unknown }).message);
		}
		return String(error);
	}
</script>

<svelte:boundary onerror={(error) => console.error('组件渲染出错', label, error)}>
	{@render children()}

	{#snippet failed(error, reset)}
		{#if !silent}
			<div
				role="alert"
				class="flex items-center gap-3 rounded-panel border border-dashed border-line bg-surface/60 px-4 py-5 text-sm"
			>
				<span class="grid size-9 shrink-0 place-items-center rounded-full bg-raised text-fg-subtle">
					<Icon name="triangle-alert" size={17} />
				</span>
				<div class="min-w-0 flex-1 text-fg-muted">
					<p>{i18n.t('boundary.failed')}</p>
					{#if dev}
						<!--
							开发时把原因也写在页面上。注意水合期抛出的错误会被 SvelteKit 规范化成 Internal Error，
							原始错误栈在控制台（Svelte 自己会打一遍）。生产只记控制台。
						-->
						<p class="mt-1 truncate text-xs text-danger">{describe(error)}</p>
					{/if}
				</div>
				<button type="button" class="btn btn-sm btn-ghost shrink-0" onclick={reset}>{i18n.t('common.retry')}</button>
			</div>
		{/if}
	{/snippet}
</svelte:boundary>
