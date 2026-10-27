<script lang="ts">
	/**
	 * 评论区（docs/04 C.4）。两种版式共用同一套状态：
	 * - panel：播放页电脑侧栏，与「选集」切换；标题与条数由侧栏标签显示
	 * - sheet：手机底部抽屉，标题由 CommentSheet 显示
	 * 两种版式都是「排序行 + 可滚动列表 + 贴底发表框」，回复时发表框变成「回复 某人」。
	 */
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onDestroy, onMount, untrack } from 'svelte';
	import type { CommentPage, CommentReceipt, CommentView, EmojiPack } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { splitLocale } from '#lib/i18n/locales.ts';
	import { featureOn, getSite } from '#lib/site.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import { toasts } from '#lib/stores/toast.svelte.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import CommentComposer from './CommentComposer.svelte';
	import CommentItem from './CommentItem.svelte';
	import ReportDialog from './ReportDialog.svelte';
	import {
		buildThreads,
		COMMENT_SORTS,
		commentsPath,
		EMPTY_EMOJIS,
		emojiIndex,
		isMine,
		mergePages,
		nextCursor,
		prependComment,
		type CommentSort,
		type ReportReason
	} from './thread.ts';

	type Props = {
		episodeId: number;
		variant?: 'panel' | 'sheet';
		/**
		 * 已加载的条数回传给页面（侧栏标签与抽屉标题上的「评论（N）」）。
		 * complete=false 表示还有下一页，调用方应显示「N+」——接口是游标分页，拿不到总数。
		 */
		oncount?: (count: number, complete: boolean) => void;
		class?: string;
	};
	let { episodeId, variant = 'panel', oncount, class: className = '' }: Props = $props();

	const PAGE_SIZE = 20;
	/** 删除给 5 秒撤销窗口：窗口内只是先隐藏，到点才真的调接口（接口没有还原，与「清空历史」同一套做法） */
	const UNDO_MS = 5000;
	/** 回复默认折起（与主流短视频评论区一致），点「展开 N 条回复」才展开 */

	const i18n = getI18n();
	const site = $derived(getSite());
	const sheet = $derived(variant === 'sheet');

	let sort = $state<CommentSort>('latest');
	let items = $state<CommentView[]>([]);
	let cursor = $state<string | undefined>(undefined);
	/** init：首屏骨架；idle / loading / error：翻页状态 */
	let phase = $state<'init' | 'idle' | 'loading' | 'error'>('init');
	/** 本次会话里自己发的待审评论：列表接口不含待审内容，只在本地回显 */
	let minePending = $state<CommentView[]>([]);
	let postedIDs = $state<number[]>([]);
	let likedIDs = $state<number[]>([]);
	/** 等撤销窗口结束才真删：这些评论先从界面上消失 */
	let removingIDs = $state<number[]>([]);
	/** 展开了全部回复的楼层 */
	let expandedIDs = $state<number[]>([]);
	let emojis = $state<ReadonlyMap<string, string>>(EMPTY_EMOJIS);
	let replyTo = $state<CommentView | null>(null);
	let reportTarget = $state<CommentView | null>(null);
	let posting = $state(false);
	let reporting = $state(false);
	let sentinel = $state<HTMLDivElement>();
	let scroller = $state<HTMLDivElement>();

	let controller: AbortController | null = null;
	let timers: Record<number, ReturnType<typeof setTimeout>> = {};
	/** 已经取过数的分集：换集时才清掉待审回显与点赞态，换排序不清 */
	let loadedEpisode = 0;

	const shown = $derived(items.filter((c) => !removingIDs.includes(c.id)));
	const threads = $derived(buildThreads(shown));
	const count = $derived(shown.length + minePending.length);
	/** 游客互动关闭时游客不能发表 / 回复 / 举报，先引导登录（后端同样会拒） */
	const canPost = $derived(session.loggedIn || featureOn(site.features, 'guest_interaction'));

	// 分集或排序变了：丢掉旧数据、取消进行中的请求，从第一页重新开始。
	// load() 内部会读若干 $state，用 untrack 隔开，否则翻页写状态会把这个 effect 再触发一次
	$effect.pre(() => {
		const episode = episodeId;
		void sort;
		untrack(() => {
			controller?.abort();
			items = [];
			cursor = undefined;
			phase = 'init';
			replyTo = null;
			expandedIDs = [];
			if (episode !== loadedEpisode) {
				loadedEpisode = episode;
				minePending = [];
				postedIDs = [];
				likedIDs = [];
				removingIDs = [];
			}
			void load(true);
		});
	});

	$effect(() => {
		const total = count;
		const complete = !cursor && phase !== 'init';
		untrack(() => oncount?.(total, complete));
	});

	// 表情映射：正文里的表情码要渲染成图，列表与输入框共用一份（接口带 ETag，可被边缘缓存 1 天）。
	// 只在挂载时取一次：放在 $effect 里并用「表情为空」当重试条件会变成死循环——
	// 后台没配表情（或命中 304）时结果恒为空，写回 state 又把 effect 自己触发一遍。
	onMount(() => {
		void import('#lib/api/client.ts')
			.then(({ api }) => api.get<{ packs: EmojiPack[] }>('/api/v1/emojis'))
			.then((data) => {
				if (data?.packs) {
					emojis = emojiIndex(data.packs);
				}
			})
			.catch(() => undefined);
	});

	// 无限滚动：接近底部时自动取下一页。列表在自己的滚动容器里（抽屉 / 侧栏），
	// 所以要把容器作为 IntersectionObserver 的 root，否则视口判定不准。
	// 下面的按钮是键盘与无 IntersectionObserver 时的兜底。
	$effect(() => {
		const el = sentinel;
		const root = scroller;
		if (!el || !root || !cursor || phase !== 'idle') {
			return;
		}
		const observer = new IntersectionObserver((entries) => entries.some((e) => e.isIntersecting) && void load(false), {
			root,
			rootMargin: '300px 0px'
		});
		observer.observe(el);
		return () => observer.disconnect();
	});

	onDestroy(() => {
		controller?.abort();
		// 还在撤销窗口里的删除立刻执行：否则用户以为删掉了，刷新又回来了
		for (const [id, timer] of Object.entries(timers)) {
			clearTimeout(timer);
			void remove(Number(id));
		}
		timers = {};
	});

	async function load(first: boolean) {
		// 没有下一页时必须拦住：接口到底时下发的是空游标，放过去会丢掉 cursor 参数、
		// 反复重新请求第一页（无限滚动每次都重新武装 → 请求风暴 + 列表底部反复重排）
		if (!first && (phase === 'loading' || !cursor)) {
			return;
		}
		phase = first ? 'init' : 'loading';
		const next = new AbortController();
		controller = next;
		try {
			const { api } = await import('#lib/api/client.ts');
			const sent = first ? undefined : cursor;
			const path = commentsPath(episodeId, sort, sent ?? null, PAGE_SIZE);
			const data = await api.get<CommentPage>(path, next.signal);
			items = first ? data.list : mergePages(items, data.list);
			cursor = nextCursor(data.next_cursor, sent);
			phase = 'idle';
		} catch (err) {
			if (!next.signal.aborted) {
				phase = 'error';
			}
			void err;
		}
	}

	/** 游客互动关闭时，发表 / 回复 / 举报先去登录（与剧集互动按钮一致） */
	function toLogin() {
		const next = splitLocale(page.url.pathname).path + page.url.search;
		void goto(i18n.path(`/login?next=${encodeURIComponent(next)}`));
	}

	async function post(content: string) {
		const parent = replyTo;
		posting = true;
		try {
			const { api } = await import('#lib/api/client.ts');
			const receipt = await api.post<CommentReceipt>(`/api/v1/episodes/${episodeId}/comments`, {
				content,
				...(parent ? { parent_id: parent.id } : {})
			});
			const view: CommentView = {
				id: receipt.id,
				...(receipt.parent_id ? { parent_id: receipt.parent_id } : {}),
				// 用回执里的正文：命中「打码」屏蔽词时后端存的是替换后的文本
				content: receipt.content,
				image_count: 0,
				like_count: receipt.like_count,
				created_at: receipt.created_at,
				author: {
					is_guest: !session.loggedIn,
					...(session.me ? { user_id: session.me.user.id } : {}),
					nickname: session.me?.user.nickname || i18n.t('comment.mine')
				}
			};
			postedIDs = [...postedIDs, receipt.id];
			replyTo = null;
			if (receipt.status === 'pending') {
				// 命中「先送审核」屏蔽词，或后台开了先审后显：列表接口取不到，只在本地标「审核中」
				minePending = [view, ...minePending];
				toasts.show(i18n.t('comment.pendingHint'));
			} else {
				items = prependComment(items, view);
				if (parent) {
					expandedIDs = [...expandedIDs, parent.id];
				}
				toasts.show(i18n.t('comment.posted'), { tone: 'success' });
			}
			scroller?.scrollTo({ top: 0, behavior: 'smooth' });
		} catch (err) {
			toasts.show(i18n.error(errorCode(err)), { tone: 'error' });
		} finally {
			posting = false;
		}
	}

	async function like(item: CommentView) {
		// 点赞只对注册用户开放（游客点赞后端返回 401），也没有取消点赞接口，所以点过就定住
		if (!session.loggedIn) {
			toasts.show(i18n.t('comment.likeNeedUser'));
			toLogin();
			return;
		}
		if (likedIDs.includes(item.id)) {
			return;
		}
		likedIDs = [...likedIDs, item.id];
		try {
			const { api } = await import('#lib/api/client.ts');
			await api.post(`/api/v1/comments/${item.id}/like`);
		} catch (err) {
			likedIDs = likedIDs.filter((id) => id !== item.id);
			toasts.show(i18n.error(errorCode(err)), { tone: 'error' });
		}
	}

	function askDelete(item: CommentView) {
		minePending = minePending.filter((c) => c.id !== item.id);
		removingIDs = [...removingIDs, item.id];
		timers[item.id] = setTimeout(() => {
			delete timers[item.id];
			void remove(item.id);
		}, UNDO_MS);
		toasts.show(i18n.t('comment.deleted'), {
			duration: UNDO_MS,
			action: { label: i18n.t('common.undo'), run: () => undoDelete(item) }
		});
	}

	function undoDelete(item: CommentView) {
		const timer = timers[item.id];
		if (timer === undefined) {
			return; // 窗口已过，已经真删了
		}
		clearTimeout(timer);
		delete timers[item.id];
		removingIDs = removingIDs.filter((id) => id !== item.id);
		if (!items.some((c) => c.id === item.id)) {
			minePending = [item, ...minePending];
		}
	}

	async function remove(id: number) {
		try {
			const { api } = await import('#lib/api/client.ts');
			await api.del(`/api/v1/comments/${id}`);
			items = items.filter((c) => c.id !== id);
		} catch (err) {
			removingIDs = removingIDs.filter((x) => x !== id);
			toasts.show(i18n.error(errorCode(err)), { tone: 'error' });
		}
	}

	async function report(reason: ReportReason, detail: string) {
		const target = reportTarget;
		if (!target) {
			return;
		}
		if (!canPost) {
			reportTarget = null;
			toLogin();
			return;
		}
		reporting = true;
		try {
			const { api } = await import('#lib/api/client.ts');
			await api.post('/api/v1/reports', {
				target_type: 'comment',
				target_id: target.id,
				reason,
				...(detail ? { detail } : {})
			});
			reportTarget = null;
			toasts.show(i18n.t('comment.reportDone'), { tone: 'success' });
		} catch (err) {
			toasts.show(i18n.error(errorCode(err)), { tone: 'error' });
		} finally {
			reporting = false;
		}
	}

	function startReply(item: CommentView) {
		if (!canPost) {
			toLogin();
			return;
		}
		replyTo = item;
	}

	function errorCode(err: unknown): string {
		return err && typeof err === 'object' && 'code' in err ? String((err as { code: unknown }).code) : 'NETWORK';
	}

	const pad = $derived(sheet ? 'px-4' : '');
</script>

<section class="flex min-h-0 flex-col {className}" aria-label={i18n.t('comment.title')}>
	<div class="flex shrink-0 items-center gap-1.5 pb-2 {pad}">
		{#each COMMENT_SORTS as value (value)}
			<button type="button" class="chip" aria-pressed={sort === value} onclick={() => (sort = value)}>
				{i18n.t(value === 'hot' ? 'comment.sortHot' : 'comment.sortLatest')}
			</button>
		{/each}
	</div>

	<div bind:this={scroller} class="min-h-0 flex-1 overflow-y-auto overscroll-contain {sheet ? 'px-4' : 'pe-1'}">
		{#if phase === 'init'}
			<!-- 骨架与真实条目同高（头像 38 + 两行正文），内容到位时不跳动 -->
			<ul class="flex flex-col gap-5 py-1" role="status" aria-busy="true" aria-label={i18n.t('common.loading')}>
				{#each { length: 4 }, index (index)}
					<li class="flex gap-2.5" aria-hidden="true">
						<span class="skeleton size-9.5 shrink-0 rounded-full"></span>
						<span class="flex flex-1 flex-col gap-2 py-1">
							<span class="skeleton h-3 w-20"></span>
							<span class="skeleton h-3.5 w-11/12"></span>
						</span>
					</li>
				{/each}
			</ul>
		{:else if count === 0 && phase !== 'error'}
			<EmptyState icon="message-circle" title={i18n.t('comment.empty')} description={i18n.t('comment.emptyDesc')} compact />
		{:else}
			<ul class="flex flex-col gap-4 py-1">
				{#each minePending as item (item.id)}
					<li>
						<CommentItem {item} {emojis} mine liked={false} pending ondelete={askDelete} />
					</li>
				{/each}
				{#each threads as thread (thread.root.id)}
					{@const expanded = expandedIDs.includes(thread.root.id)}
					<li>
						<CommentItem
							item={thread.root}
							{emojis}
							mine={isMine(thread.root, session.me?.user.id, postedIDs)}
							liked={likedIDs.includes(thread.root.id)}
							onlike={like}
							onreply={startReply}
							ondelete={askDelete}
							onreport={(item) => (reportTarget = item)}
						/>
						{#if thread.replies.length > 0}
							<ul class="mt-2.5 ms-12 flex flex-col gap-3.5">
								{#if expanded}
									{#each thread.replies as reply (reply.id)}
										<li>
											<CommentItem
												item={reply}
												{emojis}
												nested
												mine={isMine(reply, session.me?.user.id, postedIDs)}
												liked={likedIDs.includes(reply.id)}
												onlike={like}
												ondelete={askDelete}
												onreport={(item) => (reportTarget = item)}
											/>
										</li>
									{/each}
								{/if}
								<li>
									<button
										type="button"
										class="inline-flex items-center gap-1 py-1 text-xs font-medium text-fg-muted transition-colors hover:text-fg"
										aria-expanded={expanded}
										onclick={() =>
											(expandedIDs = expanded
												? expandedIDs.filter((id) => id !== thread.root.id)
												: [...expandedIDs, thread.root.id])}
									>
										{expanded ? i18n.t('comment.hideReplies') : i18n.t('comment.showReplies', { n: thread.replies.length })}
										<Icon name="chevron-down" size={13} class={expanded ? 'rotate-180' : ''} />
									</button>
								</li>
							</ul>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}

		<div bind:this={sentinel} class="flex justify-center py-3">
			{#if phase === 'loading'}
				<span class="flex items-center gap-2 text-xs text-fg-subtle" role="status">
					<Icon name="loader-circle" size={14} class="animate-spin" />
					{i18n.t('common.loading')}
				</span>
			{:else if phase === 'error'}
				<button type="button" class="btn btn-sm btn-ghost" onclick={() => load(items.length === 0)}>
					<Icon name="refresh-cw" size={15} />
					{i18n.t('common.loadFailedRetry')}
				</button>
			{:else if cursor}
				<button type="button" class="btn btn-sm btn-ghost" onclick={() => load(false)}>{i18n.t('common.loadMore')}</button>
			{:else if count > PAGE_SIZE}
				<span class="text-xs text-fg-subtle">{i18n.t('common.noMore')}</span>
			{/if}
		</div>
	</div>

	<!-- 贴底发表框：回复时同一个框变成「回复 某人」，列表不会因为插入输入框而跳动 -->
	<div
		class="shrink-0 border-t border-line pt-3 {pad} {sheet ? 'pb-[max(0.75rem,var(--safe-bottom))]' : ''}"
	>
		{#if canPost}
			<CommentComposer
				placeholder={replyTo ? i18n.t('comment.replyTo', { name: replyTo.author.nickname }) : i18n.t('comment.placeholder')}
				busy={posting}
				focusOnMount={replyTo !== null}
				oncancel={replyTo ? () => (replyTo = null) : undefined}
				onsubmit={post}
			/>
		{:else}
			<button type="button" class="btn btn-sm btn-ghost w-full" onclick={toLogin}>
				<Icon name="user" size={15} />
				{i18n.t('comment.loginFirst')}
			</button>
		{/if}
	</div>
</section>

<ReportDialog open={reportTarget !== null} busy={reporting} onclose={() => (reportTarget = null)} onsubmit={report} />
