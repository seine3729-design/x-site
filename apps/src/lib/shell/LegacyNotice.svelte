<script lang="ts">
	import { onMount } from 'svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';

	/*
	 * 浏览器过旧的提示（docs/04 A.6）。
	 *
	 * 判定依据是 color-mix：Tailwind 4 的配色与透明度工具类都建在它和 @property 上，
	 * 不支持它（Safari 16.1 及更早、Chrome 110 及更早）时整站只能落到回退色，页面能看但不是设计稿的样子。
	 * 只在客户端挂载后判断，SSR 不输出这条，所以不影响首屏 HTML 与边缘缓存。
	 */
	const i18n = getI18n();
	const KEY = 'xs_legacy_ok';
	let show = $state(false);

	onMount(() => {
		try {
			if (localStorage.getItem(KEY)) {
				return;
			}
		} catch {
			// 隐私模式读不到本地存储：照常提示
		}
		const supported =
			typeof CSS !== 'undefined' &&
			typeof CSS.supports === 'function' &&
			CSS.supports('color', 'color-mix(in oklab, red, blue)');
		show = !supported;
	});

	function dismiss() {
		show = false;
		try {
			localStorage.setItem(KEY, '1');
		} catch {
			// 写不进去：本次会话内不再提示就行
		}
	}
</script>

{#if show}
	<div class="border-b border-warn/25 bg-warn/10 text-xs" role="status">
		<div class="page-x flex items-center gap-2 py-2">
			<Icon name="triangle-alert" size={15} class="shrink-0 text-warn" />
			<p class="min-w-0 flex-1 leading-5 text-fg">{i18n.t('legacy.notice')}</p>
			<button type="button" class="icon-btn size-8 shrink-0" aria-label={i18n.t('common.close')} onclick={dismiss}>
				<Icon name="x" size={15} />
			</button>
		</div>
	</div>
{/if}
