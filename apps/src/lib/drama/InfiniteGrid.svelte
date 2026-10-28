<script lang="ts">
	import { untrack } from 'svelte';
	import type { CursorPage, DramaCard } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import DramaGrid from './DramaGrid.svelte';

	type Props = {
		/** SSR 带回的第一页 */
		initial: CursorPage<DramaCard>;
		/** 下一页的接口地址（同源 /api/v1/...，经加密客户端请求） */
		nextPath: (cursor: string) => string;
		priorityCount?: number;
	};

	let { initial, nextPath, priorityCount = 6 }: Props = $props();

	const i18n = getI18n();
	let extra = $state<DramaCard[]>([]);
	let cursor = $state<string | null>(null);
	let phase = $state<'idle' | 'loading' | 'error'>('idle');
	let sentinel = $state<HTMLDivElement>();
	let controller: AbortController | null = null;

	/**
	 * 第一页的内容指纹。下面的重置按它走，**不能按 `initial` 这个对象的身份走**：
	 * 父组件传进来的是 SvelteKit 的 `data.result`，导航或 invalidate 之后会是一个内容相同的新对象，
	 * 按身份判断就会把已经追加的那几页全丢掉——列表塌缩回第一页、页面随之变矮，
	 * 浏览器把滚动位置钳到能滚到的最远处（往往就是顶部），用户看到的是「滑到一半忽然跳回顶部、
	 * 图片重新加载一遍」。线上排查过一次同样的症状（成因是别处，见 docs/04 D 节），这里属于同类隐患。
	 */
	const initialKey = $derived(`${initial.next_cursor ?? ''}|${initial.list.map((card) => card.id).join(',')}`);

	// 筛选、排序或关键词变了（第一页换了内容）：丢掉已追加的页、取消进行中的请求
	$effect.pre(() => {
		// 只认指纹这一个依赖；重置时读 initial 要 untrack，否则又会按对象身份重跑
		void initialKey;
		untrack(() => {
			cursor = initial.next_cursor;
			extra = [];
			phase = 'idle';
			controller?.abort();
		});
	});

	// 翻页期间榜单可能变化，同一部剧出现在两页时只留第一次出现的
	const cards = $derived([...initial.list, ...extra].filter((card, index, all) => all.findIndex((c) => c.id === card.id) === index));

	async function more() {
		if (!cursor || phase === 'loading') {
			return;
		}
		phase = 'loading';
		controller = new AbortController();
		try {
			const { api } = await import('#lib/api/client.ts');
			const next = await api.get<CursorPage<DramaCard>>(nextPath(cursor), controller.signal);
			extra = [...extra, ...next.list];
			cursor = next.next_cursor;
			phase = 'idle';
		} catch (err) {
			if (!controller.signal.aborted) {
				phase = 'error';
			}
			void err;
		}
	}

	// 无限滚动：接近底部 600px 时自动加载下一页；按钮作为键盘与无 IntersectionObserver 时的兜底
	$effect(() => {
		const el = sentinel;
		if (!el || !cursor || phase !== 'idle') {
			return;
		}
		const observer = new IntersectionObserver((entries) => entries.some((e) => e.isIntersecting) && void more(), {
			rootMargin: '600px 0px'
		});
		observer.observe(el);
		return () => observer.disconnect();
	});
</script>

<DramaGrid {cards} {priorityCount} />

<div bind:this={sentinel} class="flex justify-center py-8">
	{#if phase === 'loading'}
		<span class="flex items-center gap-2 text-sm text-fg-subtle" role="status">
			<Icon name="loader-circle" size={18} class="animate-spin" />
			{i18n.t('common.loading')}
		</span>
	{:else if phase === 'error'}
		<button type="button" class="btn btn-sm btn-ghost" onclick={more}>
			<Icon name="refresh-cw" size={15} />
			{i18n.t('common.loadFailedRetry')}
		</button>
	{:else if cursor}
		<button type="button" class="btn btn-sm btn-ghost" onclick={more}>{i18n.t('common.loadMore')}</button>
	{:else if cards.length > 12}
		<span class="text-xs text-fg-subtle">{i18n.t('common.noMore')}</span>
	{/if}
</div>
