<script lang="ts">
	/**
	 * 一条评论。版式参考主流短视频评论区：头像在左，昵称小号灰字在上、正文在下，
	 * 时间与「回复 / 删除 / 举报」在正文下面一行，点赞爱心单独在右侧一列（图标 + 数字竖排）。
	 */
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
		/** 审核中：只有发表者本人在本次会话里看得到（列表接口不含待审内容） */
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
	// 待审内容还没公开，不给点赞 / 回复 / 举报，但本人要能撤回
	const canDelete = $derived(mine && !!ondelete);
</script>

<article class="group/comment flex gap-2.5">
	<Avatar name={item.author.nickname} size={nested ? 30 : 38} class="mt-0.5" />

	<div class="min-w-0 flex-1">
		<p class="flex items-center gap-1.5 text-[13px] leading-5 text-fg-subtle">
			<span class="truncate font-medium">{item.author.nickname}</span>
			{#if mine}
				<span class="shrink-0 rounded bg-brand-soft px-1 text-[10px] font-semibold text-brand-fg">{i18n.t('comment.mine')}</span>
			{/if}
			{#if pending}
				<span class="shrink-0 rounded bg-raised px-1 text-[10px] font-semibold text-fg-muted">{i18n.t('comment.pending')}</span>
			{/if}
		</p>

		<div class="mt-0.5">
			<CommentContent text={item.content} {emojis} />
		</div>

		<!-- 操作行用小号文字按钮（与短视频评论区一致）；上下各留 8px，触摸区够 24×24（WCAG 2.2 AA） -->
		<div class="-my-1.5 flex flex-wrap items-center gap-3 text-xs text-fg-subtle">
			<span class="py-1.5 tabular">{relativeTime(item.created_at, i18n.locale)}</span>
			{#if onreply && !nested && !pending}
				<button type="button" class="py-1.5 font-medium transition-colors hover:text-fg" onclick={() => onreply?.(item)}>
					{i18n.t('comment.reply')}
				</button>
			{/if}
			{#if canDelete}
				<button type="button" class="py-1.5 font-medium transition-colors hover:text-danger" onclick={() => ondelete?.(item)}>
					{i18n.t('comment.delete')}
				</button>
			{:else if onreport && !pending}
				<!-- 举报平时淡出，悬停或键盘聚焦到这条评论时才明显，不抢正文的注意力 -->
				<button
					type="button"
					class="py-1.5 font-medium opacity-0 transition-opacity group-hover/comment:opacity-100 focus-visible:opacity-100 max-lg:opacity-100"
					onclick={() => onreport?.(item)}
				>
					{i18n.t('comment.report')}
				</button>
			{/if}
		</div>
	</div>

	{#if onlike && !pending}
		<button
			type="button"
			class="-me-1 flex min-h-11 w-11 shrink-0 flex-col items-center gap-0.5 pt-1 text-fg-subtle transition-colors hover:text-fg"
			aria-pressed={liked}
			aria-label={i18n.t(liked ? 'comment.liked' : 'comment.like')}
			onclick={() => onlike?.(item)}
		>
			<Icon name="heart" size={nested ? 16 : 18} filled={liked} class={liked ? 'text-brand-fg' : ''} />
			<span class="text-[11px] leading-none tabular {liked ? 'text-brand-fg' : ''}">
				{likes > 0 ? compactNumber(likes, i18n.locale) : ''}
			</span>
		</button>
	{/if}
</article>
