<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import { resolveBadge } from './badge.ts';

	type Props = {
		/** drama.badge：预设 id 或自定义文案，空则什么都不渲染 */
		value: string;
		/** sm 海报角标，md 列表与详情，lg 首页轮播 */
		size?: 'sm' | 'md' | 'lg';
		/** 贴在海报左上角（外角跟卡片圆角一致）；默认是浮在封面上的小标签 */
		corner?: boolean;
		class?: string;
	};

	let { value, size = 'sm', corner = false, class: className = '' }: Props = $props();

	const i18n = getI18n();
	const badge = $derived(resolveBadge(value));
	// 字符串写全，不拼 badge-{size}，Tailwind 扫源码时能看见
	const sizeCls = $derived(size === 'lg' ? 'badge-lg' : size === 'md' ? 'badge-md' : 'badge-sm');
	const iconSize = $derived(size === 'lg' ? 14 : size === 'md' ? 12 : 10);
</script>

{#if badge}
	<span class="badge {sizeCls} {badge.cls} {corner ? 'badge-corner' : ''} {className}">
		{#if badge.icon}
			<Icon name={badge.icon} size={iconSize} filled strokeWidth={0} class="opacity-90" />
		{/if}
		<span class="truncate">{badge.labelKey ? i18n.t(badge.labelKey) : badge.text}</span>
	</span>
{/if}
