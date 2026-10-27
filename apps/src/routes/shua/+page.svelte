<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import type { CursorPage, DramaCard, DramaDetail } from '#lib/api/types.ts';
	import CommentSection from '#lib/comment/CommentSection.svelte';
		import DramaActions from '#lib/drama/DramaActions.svelte';
	import { dramaPath, episodesLabel, playPath } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import FeedPlayer from '#lib/shua/FeedPlayer.svelte';
	import { featureOn, getSite } from '#lib/site.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import Sheet from '#lib/ui/Sheet.svelte';

	let { data } = $props();
	const i18n = getI18n();
	const site = $derived(getSite());
	const commentsOn = $derived(featureOn(site.features, 'comments'));

	// —— 评论：按当前在播的那一集，手机端从底部滑上来的抽屉 ——
	let commentEpisode = $state(0);
	let commentCount = $state(0);
	let commentComplete = $state(false);
	const commentLabel = $derived(
		commentCount > 0
			? i18n.t('comment.titleCount', { n: commentComplete ? commentCount : `${commentCount}+` })
			: i18n.t('comment.title')
	);

	function openComments(episodeId: number) {
		commentCount = 0;
		commentComplete = false;
		commentEpisode = episodeId;
	}

	/** 顶部返回：有来路就退回去，直接进来的（分享链接、刷新）回首页 */
	function back() {
		if (history.length > 1) {
			history.back();
		} else {
			void goto(i18n.path('/'));
		}
	}

	let items = $state<DramaCard[]>([]);
	let cursor = $state<string | null>(null);
	let active = $state(0);
	let feed = $state<HTMLUListElement>();
	/**
	 * 电脑端舞台上方被占掉的高度（页头 + 公告条 + 旧浏览器提示…）。
	 * 以前写死 100dvh - 页头高，顶部多一条公告条，舞台底部就被挤出视口，
	 * 播放控制条跟着跑到屏幕外；而鼠标在这个列表上滚动是「切下一部」、页面不会动，
	 * 于是控制条永远点不到（实测 PC 开着公告条时必现）。所以这里按实际值算。
	 */
	let chromeTop = $state(0);
	let loadingMore = false;
	const details: Record<number, Promise<DramaDetail | null>> = {};
	/** 已取到的剧详情（当前条与上下条都要有，播放器才挂得起来） */
	let loaded = $state<Record<number, DramaDetail>>({});
	/** 挂播放器的窗口半径：1 = 上一条 + 当前 + 下一条 */
	const WINDOW = 1;

	$effect.pre(() => {
		items = data.page.list;
		cursor = data.page.next_cursor;
	});

	/** 剧详情（拿第一集的编号）按需取，并缓存；当前一条与下一条提前取好 */
	function detail(id: number): Promise<DramaDetail | null> {
		details[id] ??= import('#lib/api/client.ts')
			.then(({ api }) => api.get<DramaDetail>(`/api/v1/dramas/${id}`))
			.catch(() => null);
		return details[id];
	}

	// 当前条与上下条的详情都提前取好：播放器要靠它拿到第一集的编号才能开始选源
	$effect(() => {
		let cancelled = false;
		for (let i = active - WINDOW; i <= active + WINDOW; i += 1) {
			const item = items[i];
			if (!item || loaded[item.id]) {
				continue;
			}
			void detail(item.id).then((d) => {
				if (d && !cancelled) {
					loaded = { ...loaded, [d.id]: d };
				}
			});
		}
		if (active >= items.length - 3) {
			void loadMore();
		}
		return () => {
			cancelled = true;
		};
	});

	async function loadMore() {
		if (!cursor || loadingMore) {
			return;
		}
		loadingMore = true;
		try {
			const { api } = await import('#lib/api/client.ts');
			const path =
				data.source === 'feed'
					? `/api/v1/feed/shua?cursor=${encodeURIComponent(cursor)}`
					: `/api/v1/explore?sort=hot&limit=12&cursor=${encodeURIComponent(cursor)}`;
			const next = await api.get<CursorPage<DramaCard>>(path);
			const seen = new Set(items.map((d) => d.id));
			items = [...items, ...next.list.filter((d) => !seen.has(d.id))];
			cursor = next.next_cursor;
		} catch {
			// 下次滑到底再试
		} finally {
			loadingMore = false;
		}
	}

	function go(index: number) {
		const el = feed?.children[index] as HTMLElement | undefined;
		el?.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start' });
	}

	onMount(() => {
		// 量一次 <main> 距文档顶部的距离就是页头 + 公告条的总高；公告条被关掉或多出一条时跟着变
		const main = document.getElementById('main');
		const measure = () => {
			chromeTop = main?.offsetTop ?? 0;
		};
		measure();
		let resize: ResizeObserver | undefined;
		if (typeof ResizeObserver === 'function' && main?.parentElement) {
			resize = new ResizeObserver(measure);
			resize.observe(main.parentElement);
		}
		window.addEventListener('resize', measure);

		// 哪一条占屏幕一大半就是当前条（上下滑由原生滚动 + scroll-snap 完成，60fps，不用 JS 动画）
		const observer = new IntersectionObserver(
			(entries) => {
				for (const entry of entries) {
					if (entry.isIntersecting) {
						active = Number((entry.target as HTMLElement).dataset.index);
					}
				}
			},
			{ root: feed, threshold: 0.6 }
		);
		const observeAll = () => feed?.querySelectorAll('[data-index]').forEach((el) => observer.observe(el));
		observeAll();
		const mutation = new MutationObserver(observeAll);
		if (feed) {
			mutation.observe(feed, { childList: true });
		}
		// 电脑端 ↑/↓ 换一部（docs/04 B.6.3）
		const onKey = (event: KeyboardEvent) => {
			if ((event.target as HTMLElement | null)?.closest('input, textarea')) {
				return;
			}
			if (event.key === 'ArrowDown') {
				event.preventDefault();
				go(Math.min(items.length - 1, active + 1));
			} else if (event.key === 'ArrowUp') {
				event.preventDefault();
				go(Math.max(0, active - 1));
			}
		};
		window.addEventListener('keydown', onKey);
		return () => {
			observer.disconnect();
			mutation.disconnect();
			resize?.disconnect();
			window.removeEventListener('resize', measure);
			window.removeEventListener('keydown', onKey);
		};
	});
</script>

<svelte:head>
	<title>{i18n.t('nav.shua')} - {page.data.site.name}</title>
	<meta name="description" content={i18n.t('shua.description')} />
</svelte:head>

<h1 class="sr-only">{i18n.t('nav.shua')}</h1>

{#if items.length === 0}
	<div class="page-x"><EmptyState icon="clapperboard" title={i18n.t('home.emptyTitle')} description={i18n.t('home.emptyDesc')} /></div>
{:else}
	<!--
	  --shua-h 是舞台的实际高度：视口（svh，不含手机动态工具栏）减去上方被占的高度，
	  再留 2.5rem 余量给底部播放控制条，保证它一直在视口里、鼠标够得到。
	  宽度由高度按 9:16 推出来，所以两者共用这一个变量。
	-->
	<div
		style="--shua-h: calc(100svh - {chromeTop || 64}px - 2.5rem)"
		class="fixed inset-x-0 top-0 bottom-[calc(var(--tabbar-h)+var(--safe-bottom))] z-30 bg-black lg:relative lg:inset-auto lg:z-auto lg:mx-auto lg:h-(--shua-h) lg:w-[calc(var(--shua-h)*0.5625)] lg:max-w-[min(40rem,90vw)] lg:bg-transparent lg:py-0"
	>
		<ul bind:this={feed} class="no-scrollbar h-full snap-y snap-mandatory overflow-y-scroll overscroll-contain lg:rounded-panel">
			{#each items as drama, index (drama.id)}
				<li class="relative h-full snap-start snap-always overflow-hidden bg-black lg:rounded-panel" data-index={index}>
					<!-- 底部信息与右侧按钮列。交给播放器渲染在它内部（见 FeedPlayer 的 info 说明）：
					     画在播放器外面会整层盖住播放器，吃掉进度条与下一集的点击。
					     手机端留出底部 7rem：下面是播放器自己的进度条（约 52px）与下一集按钮（约 96px） -->
					<!-- 参数类型要显式写：这个 snippet 是先具名声明、再用 {info} 传给 FeedPlayer 的，
					     不是内联在组件标签里，svelte-check 推不出参数类型（会报 implicit any） -->
					{#snippet info({
						hasNext,
						onnext,
						episodeId,
						episodeNumber
					}: {
						hasNext: boolean;
						onnext: () => void;
						episodeId: number;
						episodeNumber: number;
					})}
						<!-- 手机端顶部返回条：电脑端有页头与左右切换按钮，不需要 -->
						<div
							class="pointer-events-none absolute inset-x-0 top-0 z-10 flex items-center gap-2 bg-gradient-to-b from-black/60 to-transparent px-2 pt-[max(env(safe-area-inset-top),0.5rem)] pb-6 text-white lg:hidden"
						>
							<button
								type="button"
								class="pointer-events-auto grid size-10 shrink-0 place-items-center rounded-full"
								aria-label={i18n.t('nav.back')}
								onclick={back}
							>
								<Icon name="chevron-left" size={26} class="rtl:rotate-180" />
							</button>
							<p class="min-w-0 flex-1 truncate text-[15px] font-semibold">{i18n.t('nav.shua')}</p>
						</div>
						<div
							class="pointer-events-none absolute inset-x-0 bottom-0 z-10 bg-gradient-to-t from-black/85 via-black/40 to-transparent px-4 pt-16 pb-14 text-white lg:pb-20"
						>
							<!-- 剧名与集数：右侧让出按钮列的宽度 -->
							<div class="pointer-events-auto me-16">
								<a href={i18n.path(dramaPath(drama.id))} class="line-clamp-1 text-lg font-bold">{drama.title}</a>
								<p class="mt-1 text-[13px] text-white/75">{episodesLabel(i18n, drama)}</p>
							</div>
							<!-- 「看全集」与「下一集」同一行：左右各一个，中间撑开 -->
							<div class="pointer-events-auto mt-3 flex items-center gap-3">
								<a
									href={i18n.path(playPath(drama.id, 1))}
									class="inline-flex h-10 items-center gap-2 rounded-full bg-white/15 px-4 text-sm font-semibold hover:bg-white/25"
								>
									<Icon name="list-video" size={17} />{i18n.t('shua.watchAll')}
									<Icon name="chevron-right" size={15} class="rtl:rotate-180" />
								</a>
								<span class="flex-1"></span>
								{#if hasNext}
									<!-- 电脑端的下一集在播放器控制条里，这里只给手机 -->
									<button
										type="button"
										class="inline-flex h-10 items-center gap-1.5 rounded-full bg-brand px-4 text-sm font-semibold text-on-brand shadow-pop lg:hidden"
										onclick={onnext}
									>
										{i18n.t('player.next')}
										<Icon name="skip-forward" size={16} filled strokeWidth={1.75} />
									</button>
								{/if}
							</div>
						</div>
						<div class="absolute end-2 bottom-28 z-10 lg:bottom-32">
							<DramaActions
								{drama}
								variant="rail"
								oncomment={commentsOn && episodeId > 0 ? () => openComments(episodeId) : undefined}
								commentCount={commentEpisode === episodeId ? commentCount : 0}
								shareUrl={new URL(i18n.path(playPath(drama.id, episodeNumber || 1)), page.url.origin).href}
								shareSubtitle={i18n.t('episode.label', { n: episodeNumber || 1 })}
							/>
						</div>
					{/snippet}

					<!-- 当前条与上下各一条都挂播放器（抖音式预加载）：窗口外的只留封面，滑进窗口才开始取流 -->
					{#if Math.abs(index - active) <= WINDOW && loaded[drama.id]}
						<!-- 模糊氛围背景只给窗口内这几条：它是全屏 blur 合成层，给每一条都画会让列表越刷越卡 -->
						<div class="absolute inset-0 opacity-40" aria-hidden="true">
							<Poster cover={drama.cover_portrait} title={drama.title} class="absolute! inset-0 size-full scale-125 blur-2xl" />
						</div>
						<div class="absolute inset-0">
							<FeedPlayer drama={loaded[drama.id]!} active={index === active} onended={() => go(index + 1)} {info} />
						</div>
					{:else}
						<div class="absolute inset-x-0 top-1/2 mx-auto w-3/5 max-w-64 -translate-y-1/2 overflow-hidden rounded-card shadow-pop">
							<Poster cover={drama.cover_portrait} title={drama.title} priority={index === 0} />
						</div>
						{#if index === active}
							<div class="absolute inset-0 grid place-items-center"><Icon name="loader-circle" size={40} class="animate-spin text-white/80" /></div>
						{/if}
						<!-- 窗口外只放纯文字标题：不画模糊背景、不挂互动按钮，滑过时没有合成开销 -->
						<div
							class="pointer-events-none absolute inset-x-0 bottom-0 z-10 bg-gradient-to-t from-black/85 via-black/40 to-transparent px-4 pt-16 pb-8 text-white"
						>
							<p class="line-clamp-1 text-lg font-bold">{drama.title}</p>
							<p class="mt-1 text-[13px] text-white/75">{episodesLabel(i18n, drama)}</p>
						</div>
					{/if}
				</li>
			{/each}
		</ul>
		<!-- 电脑端上下切换按钮 -->
		<div class="absolute end-[-4.5rem] top-1/2 hidden -translate-y-1/2 flex-col gap-3 lg:flex">
			<button type="button" class="icon-btn size-12 bg-raised" aria-label={i18n.t('shua.prev')} disabled={active === 0} onclick={() => go(active - 1)}>
				<Icon name="chevron-up" size={22} />
			</button>
			<button type="button" class="icon-btn size-12 bg-raised" aria-label={i18n.t('shua.next')} disabled={active >= items.length - 1} onclick={() => go(active + 1)}>
				<Icon name="chevron-down" size={22} />
			</button>
		</div>
	</div>
{/if}

<!-- 评论抽屉：内容在打开时才挂载 -->
<Sheet fill open={commentEpisode > 0} title={commentLabel} onclose={() => (commentEpisode = 0)}>
	{#if commentEpisode > 0}
		<CommentSection
			class="flex-1"
			variant="sheet"
			episodeId={commentEpisode}
			oncount={(n, complete) => {
				commentCount = n;
				commentComplete = complete;
			}}
		/>
	{/if}
</Sheet>
