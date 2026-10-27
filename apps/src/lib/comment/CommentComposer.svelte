<script lang="ts">
	/**
	 * 发表框：一行胶囊输入，随内容长高（最多约 5 行），电脑与手机同一套。
	 * 侧栏空间紧张，所以不用大块文本域 + 独立工具条；回车发送、Shift+回车换行。
	 */
	import type { EmojiPack } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import EmojiPicker from './EmojiPicker.svelte';
	import { COMMENT_MAX, contentLength, draftState } from './thread.ts';

	type Props = {
		placeholder: string;
		busy: boolean;
		/** 回复时显示「取消」，回到发表顶层评论 */
		oncancel?: () => void;
		onsubmit: (content: string) => void;
		/** 切到回复时自动聚焦，省一次点击 */
		focusOnMount?: boolean;
	};
	let { placeholder, busy, oncancel, onsubmit, focusOnMount = false }: Props = $props();

	const i18n = getI18n();
	let draft = $state('');
	let box = $state<HTMLTextAreaElement>();
	let emojiOpen = $state(false);
	let packs = $state<EmojiPack[] | null>(null);
	let loadedEmojis = false;

	const status = $derived(draftState(draft));
	const remain = $derived(COMMENT_MAX - contentLength(draft.trim()));

	$effect(() => {
		if (focusOnMount) {
			box?.focus();
		}
	});

	// 随内容长高，不出现内部滚动条（上限与样式里的 max-h-30 一致）
	$effect(() => {
		const el = box;
		if (!el) {
			return;
		}
		void draft;
		el.style.height = 'auto';
		el.style.height = `${Math.min(el.scrollHeight, 120)}px`;
	});

	/** 表情面板首次展开时才取表情（接口带 ETag，可被边缘缓存 1 天） */
	async function toggleEmoji() {
		emojiOpen = !emojiOpen;
		if (!emojiOpen || loadedEmojis) {
			return;
		}
		loadedEmojis = true;
		try {
			const { api } = await import('#lib/api/client.ts');
			// 304 时 api.get 返回 undefined（没有响应体可解），按「没取到」处理而不是炸掉
			packs = (await api.get<{ packs: EmojiPack[] }>('/api/v1/emojis'))?.packs ?? [];
		} catch {
			packs = [];
			loadedEmojis = false;
		}
	}

	/** 把表情码插到光标处，插完焦点回输入框 */
	function insert(code: string) {
		const el = box;
		const at = el?.selectionStart ?? draft.length;
		const end = el?.selectionEnd ?? at;
		draft = draft.slice(0, at) + code + draft.slice(end);
		el?.focus();
		requestAnimationFrame(() => el?.setSelectionRange(at + code.length, at + code.length));
	}

	function submit() {
		if (busy || status !== 'ok') {
			return;
		}
		onsubmit(draft.trim());
		draft = '';
		emojiOpen = false;
	}
</script>

<form
	class="flex flex-col gap-2"
	onsubmit={(event) => {
		event.preventDefault();
		submit();
	}}
>
	<div class="flex items-end gap-2">
		<div
			class="flex min-w-0 flex-1 items-end rounded-[1.375rem] border border-transparent bg-raised ps-4 pe-1 transition-colors focus-within:border-brand-fg/50"
		>
			<label class="sr-only" for="comment-draft">{placeholder}</label>
			<textarea
				bind:this={box}
				bind:value={draft}
				id="comment-draft"
				class="my-2 max-h-30 min-h-6 w-full resize-none border-0 bg-transparent p-0 text-base leading-6 text-fg placeholder:text-fg-subtle focus:outline-none"
				rows="1"
				{placeholder}
				aria-invalid={status === 'too-long' ? 'true' : undefined}
				aria-describedby="comment-draft-hint"
				onkeydown={(event) => {
					// 回车发送、Shift+回车换行（手机输入法的回车不会走到这里，仍然是换行）
					if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
						event.preventDefault();
						submit();
					}
				}}
			></textarea>
			<button
				type="button"
				class="icon-btn mb-0.5 size-9 shrink-0"
				aria-label={i18n.t('comment.emoji')}
				aria-expanded={emojiOpen}
				onclick={toggleEmoji}
			>
				<Icon name="smile" size={20} />
			</button>
		</div>
		{#if oncancel}
			<button type="button" class="btn btn-sm btn-ghost mb-0.5 shrink-0" onclick={oncancel}>{i18n.t('common.cancel')}</button>
		{/if}
		<!-- 发表按钮常驻（禁用态而不是隐藏）：出现/消失会让这一行跳动 -->
		<button type="submit" class="btn btn-sm btn-primary mb-0.5 shrink-0" disabled={busy || status !== 'ok'}>
			{#if busy}
				<Icon name="loader-circle" size={15} class="animate-spin" />
			{:else}
				<Icon name="send" size={15} />
			{/if}
			{i18n.t('comment.send')}
		</button>
	</div>

	{#if status === 'too-long'}
		<p id="comment-draft-hint" class="px-1 text-xs text-danger" aria-live="polite">{i18n.t('comment.tooLong')}</p>
	{:else if remain <= 100 && draft.trim() !== ''}
		<p id="comment-draft-hint" class="px-1 text-xs tabular text-fg-subtle" aria-live="polite">
			{i18n.t('comment.remain', { n: remain })}
		</p>
	{/if}

	{#if emojiOpen}
		<EmojiPicker {packs} onpick={insert} />
	{/if}
</form>
