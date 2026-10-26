<script lang="ts">
	import { ICONS, type IconName } from './icons.ts';

	type Props = {
		name: IconName;
		size?: number;
		class?: string;
		strokeWidth?: number;
		/** 实心（如已点赞的心形）；默认描边 */
		filled?: boolean;
		/** 有意义的独立图标才传：变成 role="img" 并作为可访问名称；装饰性图标保持 aria-hidden */
		label?: string;
	};

	let { name, size = 20, class: className = '', strokeWidth = 2, filled = false, label }: Props = $props();
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill={filled ? 'currentColor' : 'none'}
	stroke="currentColor"
	stroke-width={strokeWidth}
	stroke-linecap="round"
	stroke-linejoin="round"
	class="shrink-0 {className}"
	role={label ? 'img' : undefined}
	aria-label={label}
	aria-hidden={label ? undefined : 'true'}
	focusable="false"
>
	{#each ICONS[name] as [tag, attrs], index (index)}
		<svelte:element this={tag} xmlns="http://www.w3.org/2000/svg" {...attrs} />
	{/each}
</svg>
