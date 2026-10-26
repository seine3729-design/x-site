<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = {
		open: boolean;
		/** 对话框的可访问名称 */
		label: string;
		onclose: () => void;
		/** center：居中弹窗；sheet：手机从底部弹出、电脑居中 */
		variant?: 'center' | 'sheet';
		size?: 'sm' | 'md' | 'lg';
		/** false 时点遮罩与 Esc 不关闭（年龄确认等必须选择的弹窗） */
		dismissible?: boolean;
		class?: string;
		children: Snippet;
	};

	let {
		open,
		label,
		onclose,
		variant = 'center',
		size = 'md',
		dismissible = true,
		class: className = '',
		children
	}: Props = $props();

	let dialog = $state<HTMLDialogElement>();

	// 原生 <dialog>.showModal()：顶层渲染、焦点限制在弹窗内、Esc 关闭，都由浏览器负责
	$effect(() => {
		if (!dialog) {
			return;
		}
		if (open && !dialog.open) {
			dialog.showModal();
			document.documentElement.style.overflow = 'hidden';
		} else if (!open && dialog.open) {
			dialog.close();
		}
		return () => {
			document.documentElement.style.overflow = '';
		};
	});

	const width = { sm: 'sm:max-w-md', md: 'sm:max-w-lg', lg: 'sm:max-w-2xl' };
</script>

<dialog
	bind:this={dialog}
	aria-label={label}
	class="text-fg backdrop:bg-black/50 backdrop:backdrop-blur-md open:animate-rise {variant === 'sheet'
		? 'm-0 mt-auto max-h-[85dvh] w-full max-w-none rounded-t-panel sm:m-auto sm:max-h-[80dvh] sm:rounded-panel'
		: 'm-auto max-h-[90dvh] w-[calc(100%-2rem)] rounded-panel'} {width[size]} border border-line bg-overlay p-0 shadow-pop {className}"
	onclose={() => {
		document.documentElement.style.overflow = '';
		if (open) {
			onclose();
		}
	}}
	oncancel={(event) => {
		if (!dismissible) {
			event.preventDefault();
		}
	}}
	onclick={(event) => {
		// 点在遮罩上（事件目标是 dialog 自身而不是内容）时关闭
		if (dismissible && event.target === dialog) {
			dialog?.close();
		}
	}}
>
	<div class="max-h-[inherit] overflow-y-auto p-6 {variant === 'sheet' ? 'pb-[calc(1.5rem+var(--safe-bottom))]' : ''}">
		{@render children()}
	</div>
</dialog>
