<script lang="ts">
	import type { CommentView } from '#lib/api/types.ts';
	import { compactNumber, relativeTime } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import Avatar from '#lib/ui/Avatar.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import CommentContent from './CommentContent.svelte';

	type Props = {
		item: CommentView;
		emojis: ReadonlyMap<string, string>;
		mine: boolean;
		liked: boolean;
		/** 审核中：只有发表者本人在本次会话里看得到（接口列表不含待审内容） */
		pending?: boolean;
		/** 回复不再嵌套下一层，也不显示回复按钮 */
		nested?: boolean;
		onlike?: (item: CommentView) => void;
		onreply?: (item: CommentView) => void;
		ondelete?: (item: CommentView) => void;
		onreport?: (item: CommentView) => void;
	};
	let { item, emojis, mine, liked, pending = false, nested = false, onlike, onreply, ondelete, onreport }: Props = $props();

	const i18n = getI18n();
	const likes = $derived(item.like_count + (liked ? 1 : 0));
</script>

<article class="flex gap-3">
	<Avatar name={item.author.nickname} size={nested ? 28 : 36} />
	<div class="min-w-0 flex-1">
		<div class="flex flex-wrap items-center gap-x-2 gap-y-0.5">
			<span class="truncate text-sm font-semibold">{item.author.nickname}</span>
			{#if mine}
				<span class="rounded bg-brand-soft px-1.5 py-0.5 text-[11px] font-semibold text-brand-fg">{i18n.t('comment.mine')}</span>
			{/if}
			<span class="text-xs text-fg-subtle tabular">{relativeTime(item.created_at, i18n.locale)}</span>
			{#if pending}
				<span class="rounded bg-raised px-1.5 py-0.5 text-[11px] font-semibold text-fg-muted">{i18n.t('comment.pending')}</span>
			{/if}
		</div>

		<div class="mt-1">
			<CommentContent text={item.content} {emojis} />
		</div>

		{#if !pending}
			<div class="mt-1.5 flex flex-wrap items-center gap-1">
				{#if onlike}
					<button
						type="button"
						class="chip h-9 {liked ? 'text-brand-fg' : ''}"
						aria-pressed={liked}
						aria-label={i18n.t(liked ? 'comment.liked' : 'comment.like')}
						onclick={() => onlike?.(item)}
					>
						<Icon name="thumbs-up" size={15} filled={liked} />
						{#if likes > 0}<span class="tabular">{compactNumber(likes, i18n.locale)}</span>{/if}
					</button>
				{/if}
				{#if onreply && !nested}
					<button type="button" class="chip h-9" onclick={() => onreply?.(item)}>
						<Icon name="message-circle" size={15} />
						{i18n.t('comment.reply')}
					</button>
				{/if}
				{#if mine && ondelete}
					<button type="button" class="chip h-9" onclick={() => ondelete?.(item)}>
						<Icon name="trash-2" size={15} />
						{i18n.t('comment.delete')}
					</button>
				{:else if onreport}
					<button type="button" class="chip h-9" onclick={() => onreport?.(item)}>
						<Icon name="flag" size={15} />
						{i18n.t('comment.report')}
					</button>
				{/if}
			</div>
		{/if}
	</div>
</article>
