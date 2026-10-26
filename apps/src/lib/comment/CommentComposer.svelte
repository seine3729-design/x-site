<script lang="ts">
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
		/** 回复时自动聚焦，省一次点击 */
		focusOnMount?: boolean;
	};
	let { placeholder, busy, oncancel, onsubmit, focusOnMount = false }: Props = $props();

	const i18n = getI18n();
	let draft = $state('');
	let box = $state<HTMLTextAreaElement>();
	let emojiOpen = $state(false);
	let packs = $state<EmojiPack[] | null>(null);
	let loadedEmojis = false;

	const state = $derived(draftState(draft));
	const remain = $derived(COMMENT_MAX - contentLength(draft.trim()));

	$effect(() => {
		if (focusOnMount) {
			box?.focus();
		}
	});

	/** 表情面板首次展开时才取表情（带 ETag，可被边缘缓存 1 天） */
	async function toggleEmoji() {
		emojiOpen = !emojiOpen;
		if (!emojiOpen || loadedEmojis) {
			return;
		}
		loadedEmojis = true;
		try {
			const { api } = await import('#lib/api/client.ts');
			packs = (await api.get<{ packs: EmojiPack[] }>('/api/v1/emojis')).packs;
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
		if (busy || state !== 'ok') {
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
	<label class="sr-only" for="comment-draft">{placeholder}</label>
	<textarea
		bind:this={box}
		bind:value={draft}
		id="comment-draft"
		class="field resize-none"
		rows="2"
		{placeholder}
		aria-invalid={state === 'too-long' ? 'true' : undefined}
		aria-describedby="comment-draft-hint"
	></textarea>
	<div class="flex items-center gap-2">
		<button
			type="button"
			class="icon-btn"
			aria-label={i18n.t('comment.emoji')}
			aria-expanded={emojiOpen}
			onclick={toggleEmoji}
		>
			<Icon name="smile" size={20} />
		</button>
		<!-- 超长时才用危险色提示，平时只在接近上限时显示剩余字数，避免一直占着视觉重量 -->
		<p id="comment-draft-hint" class="min-w-0 flex-1 truncate text-xs tabular" aria-live="polite">
			{#if state === 'too-long'}
				<span class="text-danger">{i18n.t('comment.tooLong')}</span>
			{:else if remain <= 100 && draft.trim() !== ''}
				<span class="text-fg-subtle">{i18n.t('comment.remain', { n: remain })}</span>
			{/if}
		</p>
		{#if oncancel}
			<button type="button" class="btn btn-sm btn-ghost" onclick={oncancel}>{i18n.t('common.cancel')}</button>
		{/if}
		<button type="submit" class="btn btn-sm btn-primary" disabled={busy || state !== 'ok'}>
			{#if busy}
				<Icon name="loader-circle" size={15} class="animate-spin" />
			{:else}
				<Icon name="send" size={15} />
			{/if}
			{i18n.t('comment.send')}
		</button>
	</div>
	{#if emojiOpen}
		<EmojiPicker {packs} onpick={insert} />
	{/if}
</form>
