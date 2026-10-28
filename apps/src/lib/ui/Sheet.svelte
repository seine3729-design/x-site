<script lang="ts">
	/**
	 * 手机从底部滑上来的抽屉，电脑可选居中弹窗（docs/04 C.4、C.7）。
	 *
	 * 用原生 <dialog>.showModal() 而不是普通 fixed 层：播放器根节点带 isolation: isolate，
	 * 自成层叠上下文，画在它外面的 fixed 元素会被整层盖住；top layer 不受 z-index 影响。
	 * fill=true 时高度写死（不随内容变化）：内容是异步来的，内容驱动高度会让抽屉顶边一跳一跳。
	 */
	import type { Snippet } from 'svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from './Icon.svelte';

	type Props = {
		open: boolean;
		/** 标题，同时作为对话框的可访问名称 */
		title: string;
		onclose: () => void;
		/** true：固定高度 76dvh（列表类，如评论）；false：高度跟着内容 */
		fill?: boolean;
		/** true：电脑端改成居中弹窗（分享面板等内容不多的面板） */
		center?: boolean;
		children: Snippet;
	};
	let { open, title, onclose, fill = false, center = false, children }: Props = $props();

	/** 下拉超过这个距离就关闭，否则弹回 */
	const DISMISS_PX = 110;
	const CLOSE_MS = 220;

	const i18n = getI18n();
	let dialog = $state<HTMLDialogElement>();
	/** 关闭动画期间 dialog 仍然是打开的，动画跑完才真的 close() */
	let closing = $state(false);
	/** 跟手下拉的位移 */
	let drag = $state(0);
	let dragging = $state(false);
	let startY = 0;

	$effect(() => {
		const el = dialog;
		if (!el) {
			return;
		}
		if (open && !el.open) {
			drag = 0;
			closing = false;
			el.showModal();
		} else if (!open && el.open && !closing) {
			startClose();
		}
	});

	function startClose() {
		if (closing) {
			return;
		}
		if (!dialog?.open) {
			onclose();
			return;
		}
		closing = true;
		// 不等 animationend：用户开了「减弱动效」时动画是 none，事件永远不会来
		const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
		setTimeout(() => dialog?.close(), reduced ? 0 : CLOSE_MS);
	}

	function onpointerdown(event: PointerEvent) {
		dragging = true;
		drag = 0;
		startY = event.clientY;
		(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
	}

	function onpointermove(event: PointerEvent) {
		if (dragging) {
			// 只跟向下的手势；往上拉不把抽屉拽出屏幕
			drag = Math.max(0, event.clientY - startY);
		}
	}

	function onpointerup() {
		if (!dragging) {
			return;
		}
		dragging = false;
		if (drag > DISMISS_PX) {
			startClose();
		} else {
			drag = 0;
		}
	}
</script>

<dialog
	bind:this={dialog}
	class="sheet {closing ? 'sheet-closing' : ''}"
	aria-label={title}
	onclose={() => {
		closing = false;
		drag = 0;
		if (open) {
			onclose();
		}
	}}
	oncancel={(event) => {
		// Esc 也走滑下去的动画，而不是瞬间消失
		event.preventDefault();
		startClose();
	}}
	onclick={(event) => {
		if (event.target === dialog) {
			startClose();
		}
	}}
>
	<div
		class="panel {fill ? 'panel-fill' : ''} {center ? 'panel-center' : ''} {closing ? 'panel-out' : 'panel-in'}"
		style:translate={drag > 0 ? `0 ${drag}px` : null}
		style:transition={dragging ? 'none' : null}
	>
		<!--
			抓手：按住下拉关闭。touch-action: none 防止竖向手势被下面的列表滚动抢走。
			它是纯指针增强，不可聚焦、对读屏隐藏（键盘与读屏用右边的关闭按钮或 Esc）——
			一旦给它 tabindex，showModal() 会把焦点落在这里，全局 :focus-visible 的描边会铺成两条横线。
		-->
		<div class="grab" aria-hidden="true" {onpointerdown} {onpointermove} {onpointerup} onpointercancel={onpointerup}>
			<span class="bar"></span>
		</div>

		<div class="relative flex items-center justify-center px-4 pb-2">
			<h2 class="truncate text-sm font-semibold">{title}</h2>
			<!-- 抓手不可聚焦后，showModal() 的初始焦点自然落在这里：抽屉有明确的退出路径（ui-ux escape-routes） -->
			<button
				type="button"
				class="icon-btn absolute end-2 top-1/2 -translate-y-1/2"
				aria-label={i18n.t('common.close')}
				onclick={startClose}
			>
				<Icon name="x" size={20} />
			</button>
		</div>

		<div class="flex min-h-0 flex-1 flex-col">{@render children()}</div>
	</div>
</dialog>

<style>
	/* dialog 本体铺满视口、完全透明，只作为 top layer 容器；面板贴在底部 */
	.sheet {
		position: fixed;
		inset: 0;
		width: 100%;
		max-width: none;
		height: 100%;
		max-height: none;
		margin: 0;
		padding: 0;
		border: 0;
		background: transparent;
		overflow: hidden;
		color: var(--fg);
	}
	.sheet::backdrop {
		background: rgb(0 0 0 / 0.55);
	}
	.panel {
		position: absolute;
		inset-inline: 0;
		bottom: 0;
		display: flex;
		max-height: calc(100dvh - 3rem);
		flex-direction: column;
		border-top-left-radius: var(--radius-panel);
		border-top-right-radius: var(--radius-panel);
		background: var(--overlay);
		box-shadow: var(--shadow-pop);
		transition: translate 240ms var(--ease-out-soft);
		/*
		  两个属性都要声明：拖拽改 translate，进出场动画改 transform。
		  只写 translate 时进出场那一下拿不到提前提层的提示，海报之类的首屏工作一占主线程就容易丢帧。
		  别图省事把动画也改成 translate：animation-fill-mode 是 both，动画会永久接管这个属性，
		  行内的拖拽位移会被它盖掉（动画的层叠来源高于行内样式），下拉关闭直接失效。
		*/
		will-change: transform, translate;
		padding-bottom: var(--safe-bottom);
	}
	/* 列表类：高度写死，内容再怎么加载顶边都不动 */
	.panel-fill {
		height: 76dvh;
	}
	.grab {
		display: grid;
		flex-shrink: 0;
		place-items: center;
		padding: 0.625rem 0 0.375rem;
		cursor: grab;
		touch-action: none;
	}
	.bar {
		width: 2.25rem;
		height: 0.25rem;
		border-radius: 9999px;
		background: var(--line-strong);
	}
	/* 电脑端的居中弹窗：没有下拉手势，抓手收起来 */
	@media (min-width: 64rem) {
		.panel-center {
			inset: auto;
			top: 50%;
			left: 50%;
			translate: -50% -50%;
			width: min(28rem, calc(100vw - 3rem));
			max-height: calc(100dvh - 6rem);
			border-radius: var(--radius-panel);
			padding-bottom: 0;
		}
		.panel-center .grab {
			display: none;
		}
	}
	@media (prefers-reduced-motion: no-preference) {
		.panel-in {
			animation: sheet-up 300ms var(--ease-out-soft) both;
		}
		.panel-out {
			animation: sheet-down 220ms ease-in both;
		}
		.sheet::backdrop {
			animation: sheet-fade 260ms var(--ease-out-soft) both;
		}
		.sheet-closing::backdrop {
			animation: sheet-fade 220ms ease-in both reverse;
		}
		@media (min-width: 64rem) {
			.panel-center.panel-in {
				animation: pop-in 220ms var(--ease-out-soft) both;
			}
			.panel-center.panel-out {
				animation: pop-in 180ms ease-in both reverse;
			}
		}
	}
	@keyframes sheet-up {
		from {
			transform: translateY(100%);
		}
	}
	@keyframes sheet-down {
		to {
			transform: translateY(100%);
		}
	}
	@keyframes sheet-fade {
		from {
			opacity: 0;
		}
	}
	@keyframes pop-in {
		from {
			opacity: 0;
			transform: scale(0.94);
		}
	}
</style>
