<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { PUBLIC_SITE_URL } from '$app/env/public';
	import { onMount, untrack } from 'svelte';
	import DramaActions from '#lib/drama/DramaActions.svelte';
	import DramaCard from '#lib/drama/DramaCard.svelte';
	import EpisodeGrid from '#lib/drama/EpisodeGrid.svelte';
	import { dramaPath, episodesLabel, playPath } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { clock, compactNumber } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { localHistory } from '#lib/local/store.ts';
	import { PlayerController } from '#lib/player/controller.svelte.ts';
	import { reportLine } from '#lib/player/line-report.ts';
	import Player from '#lib/player/Player.svelte';
	import { session } from '#lib/stores/session.svelte.ts';
	import { toasts } from '#lib/stores/toast.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Modal from '#lib/ui/Modal.svelte';

	let { data } = $props();
	const i18n = getI18n();
	const drama = $derived(data.drama);
	const episode = $derived(data.episode);

	const player = new PlayerController({ onLineReport: reportLine });
	let playerView = $state<ReturnType<typeof Player>>();

	const playable = $derived(drama.episodes.filter((e) => e.playable));
	const index = $derived(playable.findIndex((e) => e.id === episode.id));
	const nextEpisode = $derived(index >= 0 ? playable[index + 1] : undefined);
	const prevEpisode = $derived(index > 0 ? playable[index - 1] : undefined);
	const nextDrama = $derived(drama.related[0]);
	const detailHref = $derived(i18n.path(dramaPath(drama.id)));
	const episodeLabel = $derived(i18n.t('episode.label', { n: episode.number }));

	let sheetOpen = $state(false);
	let countdown = $state<number | null>(null);

	function goEpisode(number: number) {
		void goto(i18n.path(playPath(drama.id, number)), { replace: true, reset: false });
	}

	// —— 续播进度：本机每次都记；登录用户同时上报账号（每 15 秒、暂停、切集、页面隐藏时） ——
	// 记的是「正在播的那一集」：切集时 data 已经换成新的一集，所以单独留一份快照
	type Active = { dramaId: number; episodeId: number; number: number; title: string; cover: string; latest: number; durationSec: number };
	let active: Active | null = null;
	let lastReported = 0;
	async function saveProgress() {
		const current = active;
		const position = Math.floor(player.currentTime);
		if (!current || position < 2 || player.wall) {
			return;
		}
		const entry = {
			drama_id: current.dramaId,
			episode_id: current.episodeId,
			episode_number: current.number,
			position_sec: position,
			duration_sec: Math.floor(player.duration) || current.durationSec
		};
		await localHistory.save({
			...entry,
			title: current.title,
			cover: current.cover,
			latest_episode: current.latest,
			updated_at: Date.now()
		});
		if (session.loggedIn && Math.abs(position - lastReported) >= 3) {
			lastReported = position;
			const { api } = await import('#lib/api/client.ts');
			await api.post('/api/v1/me/history', { items: [entry] }).catch(() => undefined);
		}
	}

	// 换集（含首次进入）：先记下上一集的进度，再按本机记录决定从哪里开始
	let loadedEpisode = 0;
	$effect(() => {
		const id = episode.id;
		const number = episode.number;
		untrack(() => {
			if (loadedEpisode === id) {
				return;
			}
			if (loadedEpisode !== 0) {
				void saveProgress();
			}
			loadedEpisode = id;
			active = {
				dramaId: drama.id,
				episodeId: id,
				number,
				title: drama.title,
				cover: drama.cover_portrait,
				latest: drama.latest_episode,
				durationSec: episode.duration_sec
			};
			countdown = null;
			lastReported = 0;
			void localHistory.get(drama.id).then((saved) => {
				const duration = saved?.duration_sec ?? episode.duration_sec;
				const resumeAt =
					saved && saved.episode_number === number && saved.position_sec > 5 && saved.position_sec < duration - 10
						? saved.position_sec
						: 0;
				void player.load(id, resumeAt);
				if (resumeAt > 0) {
					toasts.show(i18n.t('player.resumed', { time: clock(resumeAt) }), {
						action: { label: i18n.t('player.restart'), run: () => player.seek(0) }
					});
				}
			});
		});
	});

	// 把写盘挪出「点一下」这一帧：暂停时立刻 saveProgress() 会在点击当帧写 IndexedDB、
	// 登录时还多发一个请求，点暂停就会明显卡一下。连续点来点去也只在最后留一次写入。
	let saveTimer: ReturnType<typeof setTimeout> | undefined;
	function scheduleSave() {
		clearTimeout(saveTimer);
		saveTimer = setTimeout(() => void saveProgress(), 600);
	}

	$effect(() => {
		// 只读 phase：saveProgress() 里会读 currentTime 等状态，同步调用会被这个 effect 追踪，
		// 导致播放中每秒若干次重建下面的定时器
		const phase = player.phase;
		if (phase !== 'playing') {
			if (phase === 'paused') {
				scheduleSave();
			}
			return;
		}
		const timer = setInterval(() => void saveProgress(), 15_000);
		return () => clearInterval(timer);
	});

	// 播完一集：3 秒倒计时自动连播下一集（可取消）
	$effect(() => {
		if (player.phase !== 'ended') {
			return;
		}
		void saveProgress();
		if (!nextEpisode) {
			return;
		}
		countdown = 3;
		const timer = setInterval(() => {
			if (countdown === null) {
				clearInterval(timer);
				return;
			}
			countdown -= 1;
			if (countdown <= 0) {
				clearInterval(timer);
				countdown = null;
				goEpisode(nextEpisode.number);
			}
		}, 1000);
		return () => clearInterval(timer);
	});

	onMount(() => {
		const onVisibility = () => {
			if (document.visibilityState === 'hidden') {
				void saveProgress();
			}
		};
		// 快捷键（docs/04 B.6.3）：空格 / K 暂停，←/→ 跳 5 秒，M 静音，F 全屏，N 下一集
		const onKey = (event: KeyboardEvent) => {
			const target = event.target as HTMLElement | null;
			if (event.metaKey || event.ctrlKey || event.altKey || target?.closest('input, textarea, select, [contenteditable="true"]')) {
				return;
			}
			const key = event.key.toLowerCase();
			if (key === ' ' || key === 'k') {
				event.preventDefault();
				playerView?.feedbackToggle();
			} else if (key === 'arrowleft') {
				event.preventDefault();
				playerView?.feedbackSeek(-5);
			} else if (key === 'arrowright') {
				event.preventDefault();
				playerView?.feedbackSeek(5);
			} else if (key === 'm') {
				playerView?.feedbackMute();
			} else if (key === 'f') {
				playerView?.toggleFullscreen();
			} else if (key === 'n' && nextEpisode) {
				goEpisode(nextEpisode.number);
			}
		};
		document.addEventListener('visibilitychange', onVisibility);
		window.addEventListener('keydown', onKey);
		return () => {
			// 离开页面：取消待写的那次，立刻落一次最终进度
			clearTimeout(saveTimer);
			void saveProgress();
			document.removeEventListener('visibilitychange', onVisibility);
			window.removeEventListener('keydown', onKey);
			player.destroy();
		};
	});

	const title = $derived(`${episodeLabel} - ${drama.title} - ${page.data.site.name}`);
	const landscape = $derived(player.aspect > 1);
</script>

<svelte:head>
	<title>{title}</title>
	<meta name="description" content={drama.summary.slice(0, 120) || drama.title} />
	<link rel="canonical" href={(PUBLIC_SITE_URL || page.url.origin) + i18n.path(playPath(drama.id, episode.number))} />
	<meta property="og:title" content="{drama.title} · {episodeLabel}" />
	<meta property="og:type" content="video.episode" />
</svelte:head>

{#snippet backdrop()}
	<Poster cover={drama.cover_portrait} title={drama.title} class="absolute! inset-0 size-full scale-125 blur-3xl" />
{/snippet}

{#snippet mobileOverlay()}
	<!-- 手机沉浸式：顶部返回与标题、右侧按钮列、底部信息与选集。
	     显示登录 / VIP 墙时叠在墙上面（只留返回与选集），用户能退出或换一集，不会被困住 -->
	<div class="pointer-events-none absolute inset-0 flex flex-col text-white lg:hidden {player.wall ? 'z-30' : 'z-10'}">
		<div class="pointer-events-auto flex items-center gap-2 bg-gradient-to-b from-black/60 to-transparent px-2 pt-[max(env(safe-area-inset-top),0.5rem)] pb-6">
			<a href={detailHref} class="grid size-10 place-items-center rounded-full" aria-label={i18n.t('nav.back')}>
				<Icon name="chevron-left" size={26} class="rtl:rotate-180" />
			</a>
			<p class="min-w-0 flex-1 truncate text-[15px] font-semibold">{drama.title}</p>
		</div>
		<div class="flex-1"></div>
		{#if !player.wall}
			<div class="pointer-events-auto absolute end-2 bottom-36 flex flex-col items-center gap-5">
				<DramaActions {drama} variant="rail" />
				<button type="button" class="flex flex-col items-center gap-1" onclick={() => (sheetOpen = true)}>
					<span class="grid size-11 place-items-center rounded-full bg-black/45"><Icon name="list-video" size={23} /></span>
					<span class="text-[11px] font-medium drop-shadow">{i18n.t('drama.episodesShort')}</span>
				</button>
			</div>
		{/if}
		<!-- 底部留出 4rem：下面是播放器抬起来的手机进度条（44px 触摸区 + 间距）。
		     播放区本身已经让开了底栏与安全区，这里不用再加 safe-area-inset-bottom -->
		<div class="pointer-events-auto bg-gradient-to-t from-black/80 via-black/40 to-transparent px-4 pt-10 pb-16">
			{#if !player.wall}
				<a href={detailHref} class="me-16 block">
					<p class="line-clamp-1 text-base font-bold">{drama.title}</p>
					<p class="mt-0.5 line-clamp-2 text-[13px] leading-5 text-white/75">{drama.summary}</p>
				</a>
			{/if}
			<button
				type="button"
				class="mt-3 flex h-10 w-full items-center gap-2 rounded-xl bg-white/15 px-3 text-sm"
				onclick={() => (sheetOpen = true)}
			>
				<Icon name="layers" size={16} />
				<span class="font-semibold">{episodeLabel}</span>
				<span class="text-white/65">· {episodesLabel(i18n, drama)}</span>
				<span class="flex-1"></span>
				<span class="text-white/65">{i18n.t('drama.episodesShort')}</span>
				<Icon name="chevron-up" size={16} />
			</button>
		</div>
	</div>
{/snippet}

<div class="lg:page-x lg:grid lg:grid-cols-[minmax(0,1fr)_23.5rem] lg:gap-6 lg:pt-4 lg:pb-6 xl:grid-cols-[minmax(0,1fr)_26rem]">
	<!-- 播放区：手机铺满屏幕，电脑端居中的竖屏画框 + 模糊封面氛围背景 -->
	<section
		class="fixed inset-x-0 top-0 bottom-[calc(var(--tabbar-h)+var(--safe-bottom))] z-30 bg-black lg:relative lg:inset-auto lg:z-auto lg:grid lg:h-[calc(100dvh-var(--header-h)-2.5rem)] lg:min-h-[32rem] lg:place-items-center lg:overflow-hidden lg:rounded-panel lg:ring-1 lg:ring-line"
		aria-label={i18n.t('player.region', { title: drama.title })}
	>
		<div class="absolute inset-0 hidden opacity-50 lg:block" aria-hidden="true">{@render backdrop()}</div>
		<div
			class="relative size-full lg:overflow-hidden lg:rounded-2xl lg:shadow-pop {landscape
				? 'lg:h-auto lg:max-h-full lg:w-[calc(100%-3rem)]'
				: 'lg:h-[calc(100%-2rem)] lg:w-auto lg:max-w-full'}"
			style:aspect-ratio={player.aspect}
		>
			<Player
				bind:this={playerView}
				{player}
				title={drama.title}
				{episodeLabel}
				{detailHref}
				hasNext={!!nextEpisode}
				hasPrev={!!prevEpisode}
				onnext={() => nextEpisode && goEpisode(nextEpisode.number)}
				onprev={() => prevEpisode && goEpisode(prevEpisode.number)}
				nextDramaTitle={nextDrama?.title}
				onnextdrama={() => nextDrama && goto(i18n.path(playPath(nextDrama.id, 1)))}
				{countdown}
				oncancelcountdown={() => (countdown = null)}
				overlay={mobileOverlay}
			/>
		</div>
	</section>

	<!-- 电脑端侧栏：剧信息、操作、选集、相关推荐 -->
	<aside class="hidden min-h-0 flex-col gap-5 overflow-hidden lg:flex lg:h-[calc(100dvh-var(--header-h)-2.5rem)]">
		<div class="rounded-panel border border-line bg-surface p-5">
			<p class="text-xs font-semibold tracking-wide text-brand-fg">{episodeLabel}</p>
			<h1 class="mt-1 line-clamp-2 text-xl leading-snug font-extrabold">
				<a href={detailHref} class="hover:text-brand-fg">{drama.title}</a>
			</h1>
			<p class="mt-2 flex flex-wrap gap-x-3 text-xs text-fg-subtle">
				<span>{episodesLabel(i18n, drama)}</span>
				{#if drama.play_count > 0}<span>{i18n.t('drama.plays', { n: compactNumber(drama.play_count, i18n.locale) })}</span>{/if}
				{#if drama.year}<span>{drama.year}</span>{/if}
			</p>
			{#if drama.summary}
				<p class="mt-3 line-clamp-3 text-[13px] leading-6 text-fg-muted">{drama.summary}</p>
			{/if}
			<div class="mt-4"><DramaActions {drama} /></div>
		</div>

		<div class="flex min-h-0 flex-1 flex-col rounded-panel border border-line bg-surface p-5">
			<div class="mb-3 flex items-center justify-between">
				<h2 class="text-base font-bold">{i18n.t('drama.episodes')}</h2>
				<span class="text-xs text-fg-subtle">{episodesLabel(i18n, drama)}</span>
			</div>
			<div class="min-h-0 flex-1 overflow-y-auto pe-1">
				<EpisodeGrid dramaId={drama.id} episodes={drama.episodes} current={episode.number} dense />
				{#if drama.related.length > 0}
					<h2 class="mt-6 mb-3 text-base font-bold">{i18n.t('drama.related')}</h2>
					<ul class="grid grid-cols-3 gap-3">
						{#each drama.related.slice(0, 9) as card (card.id)}
							<li><DramaCard {card} /></li>
						{/each}
					</ul>
				{/if}
			</div>
		</div>
	</aside>
</div>

<Modal open={sheetOpen} label={i18n.t('drama.episodes')} onclose={() => (sheetOpen = false)} variant="sheet">
	<div class="mb-4 flex items-start justify-between gap-3">
		<div class="min-w-0">
			<p class="truncate text-base font-bold">{drama.title}</p>
			<p class="text-xs text-fg-subtle">{episodesLabel(i18n, drama)}</p>
		</div>
		<button type="button" class="icon-btn -me-2 -mt-1" aria-label={i18n.t('common.close')} onclick={() => (sheetOpen = false)}>
			<Icon name="x" size={20} />
		</button>
	</div>
	<EpisodeGrid
		dramaId={drama.id}
		episodes={drama.episodes}
		current={episode.number}
		dense
		groupSize={30}
		onpick={() => (sheetOpen = false)}
	/>
</Modal>
