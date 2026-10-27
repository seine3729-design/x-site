<script lang="ts">
	import { onMount, type Snippet } from 'svelte';
	import { page } from '$app/state';
	import { clock } from '#lib/format.ts';
	import AdSlot from '#lib/home/AdSlot.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { splitLocale } from '#lib/i18n/locales.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import AccessWall from './AccessWall.svelte';
	import { SPEEDS, type PlayerController } from './controller.svelte.ts';

	type Props = {
		player: PlayerController;
		title: string;
		episodeLabel: string;
		detailHref: string;
		hasNext: boolean;
		hasPrev: boolean;
		onnext: () => void;
		onprev: () => void;
		/** 手机上滑：看下一部（相关推荐第一部），没有时为空 */
		nextDramaTitle?: string;
		onnextdrama?: () => void;
		/** 播完一集后「N 秒后播放下一集」的倒计时（由页面控制） */
		countdown?: number | null;
		oncancelcountdown?: () => void;
		/** 手机沉浸式叠层（右侧按钮列、底部信息） */
		overlay?: Snippet;
		/** 背景氛围图（模糊封面） */
		backdrop?: Snippet;
		/** 刷剧流：上下滑由外层滚动处理，播放器只接单击与长按 */
		feed?: boolean;
	};

	let {
		player,
		title,
		episodeLabel,
		detailHref,
		hasNext,
		hasPrev,
		onnext,
		onprev,
		nextDramaTitle,
		onnextdrama,
		countdown = null,
		oncancelcountdown,
		overlay,
		backdrop,
		feed = false
	}: Props = $props();

	const i18n = getI18n();
	// 试看提示条点击去登录 / 开通会员，登录后回到当前页（去掉语言前缀，与 AccessWall 一致）
	const trialNext = $derived(encodeURIComponent(splitLocale(page.url.pathname).path + page.url.search));
	let video = $state<HTMLVideoElement>();
	let frame = $state<HTMLDivElement>();
	let controlsVisible = $state(true);
	let settingsOpen = $state(false);
	let feedback = $state<{ id: number; text: string; icon?: 'play' | 'pause' | 'volume-x' | 'volume-2' | 'gauge' } | null>(null);
	let swipeHint = $state<string | null>(null);
	let fullscreen = $state(false);
	/**
	 * 播放框是否太窄：竖屏视频在矮窗口里只有二三百像素宽，这时要收起非必要控件（音量条）。
	 * 断点只能按**播放器宽度**量，不能用 `xl:` 这类视口断点——视口 1280 时播放框实测只有 295px，
	 * 音量条照样显示，整行需要 339px 塞进 263px，时间就被挤到换行。
	 */
	let narrow = $state(false);
	/** 是否暂停过：暂停贴片广告位只在第一次暂停时挂载，之后靠显隐切换，不重复取素材 */
	let pausedOnce = $state(false);
	$effect(() => {
		if (player.phase === 'paused') {
			pausedOnce = true;
		}
	});
	$effect(() => {
		const el = frame;
		if (!el || typeof ResizeObserver !== 'function') {
			return;
		}
		const observer = new ResizeObserver((entries) => {
			const entry = entries[0];
			if (entry) {
				narrow = entry.contentRect.width < 380;
			}
		});
		observer.observe(el);
		return () => observer.disconnect();
	});
	let hideTimer: ReturnType<typeof setTimeout> | undefined;
	let feedbackTimer: ReturnType<typeof setTimeout> | undefined;

	onMount(() => {
		const el = video!;
		const unbind = player.bind(el);
		const onFullscreen = () => (fullscreen = document.fullscreenElement === frame);
		document.addEventListener('fullscreenchange', onFullscreen);
		return () => {
			unbind();
			document.removeEventListener('fullscreenchange', onFullscreen);
			clearTimeout(hideTimer);
			clearTimeout(feedbackTimer);
		};
	});

	function flash(text: string, icon?: NonNullable<typeof feedback>['icon']) {
		clearTimeout(feedbackTimer);
		feedback = { id: Date.now(), text, icon };
		feedbackTimer = setTimeout(() => (feedback = null), 700);
	}

	/** 播放中 2.5 秒无操作自动隐藏控制条（暂停时一直显示） */
	function poke() {
		controlsVisible = true;
		clearTimeout(hideTimer);
		hideTimer = setTimeout(() => {
			if (player.phase === 'playing' && !settingsOpen) {
				controlsVisible = false;
			}
		}, 2500);
	}

	$effect(() => {
		if (player.phase !== 'playing') {
			controlsVisible = true;
		} else {
			poke();
		}
	});

	export function toggleFullscreen() {
		if (!frame) {
			return;
		}
		if (document.fullscreenElement) {
			void document.exitFullscreen();
		} else if (frame.requestFullscreen) {
			void frame.requestFullscreen().catch(() => undefined);
		} else {
			// iPhone Safari 只支持视频元素自己的全屏
			(video as HTMLVideoElement & { webkitEnterFullscreen?: () => void })?.webkitEnterFullscreen?.();
		}
	}

	export function feedbackSeek(delta: number) {
		player.seekBy(delta);
		flash(delta > 0 ? `+${delta}s` : `${delta}s`);
	}

	export function feedbackToggle() {
		// 不再闪一个播放 / 暂停图标：暂停时画面中间本来就常驻一个圆形播放键，
		// 两者位置重合（圆角方块叠在圆形上）会看成「多了一个图标并闪一下」。
		player.toggle();
	}

	export function feedbackMute() {
		player.setMuted(!player.muted);
		flash(i18n.t(player.muted ? 'player.unmuted' : 'player.muted'), player.muted ? 'volume-2' : 'volume-x');
	}

	// —— 触屏手势（docs/04 B.6.3）：单击暂停/继续、按住 2 倍速、左右滑切集、上滑看下一部 ——
	/** 单击允许的手指漂移（px）：触屏按下到抬手总会动几个像素，太严会把正常点击判成滑动 */
	const TAP_SLOP = 14;
	let start: { x: number; y: number; t: number } | null = null;
	let holdTimer: ReturnType<typeof setTimeout> | undefined;
	let holding = false;
	let previousRate = 1;

	// —— 拖进度条 ——
	// 拖动中只更新这里的 scrubTime 并据此画进度，不逐次写 video.currentTime：
	// 加密 HLS 下每次赋值都要重新 seek、冲掉缓冲再拉切片，一次拖动几十个 input 事件必然卡死。
	// 松手（change）时才真正跳一次。拖动中也不让 timeupdate 把滑块拽回去。
	let scrubbing = $state(false);
	let scrubTime = $state(0);

	function onScrubInput(event: Event & { currentTarget: HTMLInputElement }) {
		scrubbing = true;
		scrubTime = Number(event.currentTarget.value);
	}

	function onScrubCommit() {
		if (!scrubbing) {
			return;
		}
		scrubbing = false;
		// 拖进锁定段（试看边界之后）不跳转：scrubbing 置 false 后滑块自动弹回当前播放位置，
		// 试看片段继续播，播到边界再由 AccessWall 弹登录 / 会员墙。可播区间内正常 seek。
		if (scrubTime > playableMax) {
			return;
		}
		player.seek(scrubTime);
	}

	function onPointerDown(event: PointerEvent) {
		if (event.pointerType === 'mouse') {
			return;
		}
		start = { x: event.clientX, y: event.clientY, t: performance.now() };
		holding = false;
		clearTimeout(holdTimer);
		holdTimer = setTimeout(() => {
			if (player.phase === 'playing') {
				holding = true;
				previousRate = player.rate;
				player.setRate(2);
				flash(i18n.t('player.holdSpeed'), 'gauge');
			}
		}, 450);
	}

	function onPointerMove(event: PointerEvent) {
		if (!start) {
			return;
		}
		const dx = event.clientX - start.x;
		const dy = event.clientY - start.y;
		if (Math.abs(dx) > 10 || Math.abs(dy) > 10) {
			clearTimeout(holdTimer);
		}
		if (feed) {
			return;
		}
		if (!holding && -dy > 60 && Math.abs(dy) > Math.abs(dx)) {
			swipeHint = nextDramaTitle ? i18n.t('player.releaseNextDrama', { title: nextDramaTitle }) : i18n.t('player.noMore');
		} else if (!holding && Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy)) {
			const forward = (dx < 0) !== (document.dir === 'rtl');
			swipeHint = forward
				? i18n.t(hasNext ? 'player.releaseNext' : 'player.lastEpisode')
				: i18n.t(hasPrev ? 'player.releasePrev' : 'player.firstEpisode');
		} else {
			swipeHint = null;
		}
	}

	function onPointerUp(event: PointerEvent) {
		clearTimeout(holdTimer);
		swipeHint = null;
		if (!start) {
			return;
		}
		const dx = event.clientX - start.x;
		const dy = event.clientY - start.y;
		// 单击只看「没进长按倍速」+「手指基本没移动」，不再额外要求 300ms 内抬手：
		// 之前 300ms（单击上限）到 450ms（长按下限）之间是死区，手指稍微多停一下两边都不认，
		// 用户感受就是「点暂停没反应」。TAP_SLOP 比 10px 宽一点，容忍手指自然漂移。
		const tap = !holding && Math.abs(dx) < TAP_SLOP && Math.abs(dy) < TAP_SLOP;
		start = null;
		if (holding) {
			holding = false;
			player.setRate(previousRate);
			return;
		}
		if (feed) {
			if (tap) {
				feedbackToggle();
			}
			return;
		}
		if (-dy > 80 && Math.abs(dy) > Math.abs(dx) * 1.2) {
			onnextdrama?.();
			return;
		}
		if (Math.abs(dx) > 70 && Math.abs(dx) > Math.abs(dy) * 1.2) {
			const forward = (dx < 0) !== (document.dir === 'rtl');
			if (forward && hasNext) {
				onnext();
			} else if (!forward && hasPrev) {
				onprev();
			}
			return;
		}
		if (tap) {
			if (player.autoMuted) {
				player.setMuted(false);
				return;
			}
			feedbackToggle();
		}
	}

	/** 进度条显示用的时间：拖动中跟手指走，否则跟播放位置 */
	const shownTime = $derived(scrubbing ? scrubTime : player.currentTime);
	// 进度条按「整集时长」显示而不是试看秒数：试看清单只含前 trial_sec 秒，video.duration 只有试看长度。
	// displayDuration = 进度条总长与时间分母；playableMax = 滑块可落到的最远位置（试看片段结束处）；
	// lockedPct = 锁定段（试看边界→整集末尾）在进度条上的起点百分比。整集时长未知（老产物 duration_sec=0）时回退到 video.duration。
	const trialLocked = $derived(player.trialSec > 0 && player.fullDuration > player.duration && player.duration > 0);
	const displayDuration = $derived(player.trialSec > 0 && player.fullDuration > 0 ? player.fullDuration : player.duration);
	const playableMax = $derived(trialLocked ? player.duration : displayDuration);
	const lockedPct = $derived(trialLocked && displayDuration > 0 ? (player.duration / displayDuration) * 100 : 100);
	const progress = $derived(displayDuration > 0 ? (shownTime / displayDuration) * 100 : 0);
	const bufferedPct = $derived(displayDuration > 0 ? (player.buffered / displayDuration) * 100 : 0);
	const showSpinner = $derived(player.phase === 'loading' || (player.waiting && player.phase === 'playing'));
	const lineLabel = (index: number) => player.lines[index]?.name || i18n.t('player.lineN', { n: index + 1 });
</script>

<div
	bind:this={frame}
	class="group/player relative isolate size-full overflow-hidden bg-black select-none {controlsVisible ? '' : 'cursor-none'}"
	role="region"
	aria-label={i18n.t('player.region', { title })}
	onmousemove={poke}
	onmouseleave={() => player.phase === 'playing' && !settingsOpen && (controlsVisible = false)}
>
	{#if backdrop}
		<div class="absolute inset-0 -z-10 opacity-60" aria-hidden="true">{@render backdrop()}</div>
	{/if}

	<!-- 电脑端点画面暂停 / 继续、双击全屏；手机上画面被下面的手势层盖住，点击由手势层处理 -->
	<video
		bind:this={video}
		class="absolute inset-0 size-full object-contain"
		playsinline
		preload="auto"
		disablepictureinpicture
		onclick={feedbackToggle}
		ondblclick={toggleFullscreen}
	></video>

	<!-- 触屏手势层（电脑端不拦截指针） -->
	<div
		class="absolute inset-0 z-0 lg:pointer-events-none"
		style:touch-action={feed ? 'pan-y' : 'none'}
		aria-hidden="true"
		onpointerdown={onPointerDown}
		onpointermove={onPointerMove}
		onpointerup={onPointerUp}
		onpointercancel={() => {
			clearTimeout(holdTimer);
			start = null;
			swipeHint = null;
		}}
	></div>

	{#if showSpinner}
		<div class="pointer-events-none absolute inset-0 z-10 grid place-items-center">
			<Icon name="loader-circle" size={44} class="animate-spin text-white/85" />
		</div>
	{/if}

	{#if player.phase === 'paused' && !player.wall}
		<button
			type="button"
			class="absolute top-1/2 left-1/2 z-10 grid size-16 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full bg-black/55 text-white transition-transform hover:scale-105"
			aria-label={i18n.t('player.play')}
			onclick={() => player.toggle()}
		>
			<Icon name="play" size={30} filled strokeWidth={0} class="translate-x-0.5" />
		</button>
	{/if}

	{#if player.autoMuted && player.phase === 'playing'}
		<button
			type="button"
			class="absolute start-1/2 top-[18%] z-10 flex -translate-x-1/2 items-center gap-2 rounded-full bg-black/70 px-4 py-2 text-sm text-white rtl:translate-x-1/2"
			onclick={() => player.setMuted(false)}
		>
			<Icon name="volume-x" size={16} />
			{i18n.t('player.tapUnmute')}
		</button>
	{/if}

	{#if feedback}
		{#key feedback.id}
			<div
				class="pointer-events-none absolute top-1/2 left-1/2 z-20 flex -translate-x-1/2 -translate-y-1/2 animate-fade-in items-center gap-2 rounded-2xl bg-black/55 px-4 py-3 text-base font-semibold text-white backdrop-blur"
			>
				{#if feedback.icon}<Icon name={feedback.icon} size={22} filled={feedback.icon === 'play' || feedback.icon === 'pause'} />{/if}
				{#if feedback.text}<span>{feedback.text}</span>{/if}
			</div>
		{/key}
	{/if}

	{#if swipeHint}
		<div
			class="pointer-events-none absolute inset-x-6 bottom-[30%] z-20 rounded-2xl bg-black/60 px-4 py-3 text-center text-sm font-semibold text-white backdrop-blur"
		>
			{swipeHint}
		</div>
	{/if}

	<!-- 暂停贴片广告位（docs/04 C.5）：第一次暂停时才挂载（避免每次暂停都重新取素材），
	     之后用 hidden 收起——display:none 时 IntersectionObserver 不会误报曝光 -->
	{#if pausedOnce && !player.wall}
		<div class="absolute inset-x-4 bottom-28 z-20 mx-auto max-w-md lg:bottom-20 {player.phase === 'paused' ? '' : 'hidden'}">
			<AdSlot slot="pause" />
		</div>
	{/if}

	{#if countdown !== null && hasNext}
		<div class="absolute inset-x-4 bottom-24 z-20 mx-auto flex max-w-sm items-center gap-3 rounded-2xl bg-black/70 p-3 text-white backdrop-blur lg:bottom-20">
			<span class="grid size-10 shrink-0 place-items-center rounded-full bg-brand text-lg font-bold tabular">{countdown}</span>
			<p class="min-w-0 flex-1 text-sm">{i18n.t('player.nextIn', { n: countdown })}</p>
			<button type="button" class="btn btn-sm btn-glass" onclick={oncancelcountdown}>{i18n.t('common.cancel')}</button>
			<button type="button" class="btn btn-sm btn-primary" onclick={onnext}>{i18n.t('player.playNext')}</button>
		</div>
	{/if}

	<!-- 试看中提示（游客拿到的清单只含前 trial_sec 秒）：试看片段播放全程显示，点击去登录 / 开通会员；播到头由 AccessWall 弹墙 -->
	{#if player.trialSec > 0 && !player.trialEnded && !player.wall}
		<a
			href={i18n.path(`/${player.access === 'vip' ? 'vip' : 'login'}?next=${trialNext}`)}
			class="pointer-events-auto absolute start-1/2 top-[12%] z-20 flex -translate-x-1/2 items-center gap-1.5 rounded-full bg-black/70 px-4 py-2 text-sm font-medium text-white backdrop-blur rtl:translate-x-1/2"
		>
			<Icon name={player.access === 'vip' ? 'crown' : 'lock'} size={14} />
			{i18n.t(player.access === 'vip' ? 'player.trialNoticeVip' : 'player.trialNotice')}
		</a>
	{/if}

	{#if player.wall}
		<AccessWall wall={player.wall} trialEnded={player.trialEnded} onretry={() => player.retry()} {detailHref} />
	{/if}

	{#if overlay}
		{@render overlay()}
	{/if}

	<!-- 手机端进度条（刷剧与普通播放页统一）：抬离底部，外层留 44px 触摸区（细轨道只是视觉），
	     拖动中轨道加粗、滑块显形。原来普通播放页只有贴着屏幕底的 1px 装饰条，看不清也拖不动。
	     两种播放区的容器都已经让开 tabbar 与安全区，这里只留一点间距 -->
	<div class="absolute inset-x-0 bottom-2 z-20 px-4 lg:hidden">
		<div class="relative flex h-11 items-center">
			<div class="absolute inset-x-0 top-1/2 -translate-y-1/2 rounded-full bg-white/25 transition-[height] {scrubbing ? 'h-1.5' : 'h-1'}">
				<div class="absolute inset-y-0 start-0 rounded-full bg-white/40" style:width="{bufferedPct}%"></div>
				<div class="absolute inset-y-0 start-0 rounded-full bg-brand" style:width="{progress}%"></div>
				{#if lockedPct < 100}
					<div class="locked-zone absolute inset-y-0 end-0 rounded-e-full" style:width="{100 - lockedPct}%" aria-hidden="true"></div>
				{/if}
			</div>
			<input
				type="range"
				min="0"
				max={displayDuration || 0}
				step="0.1"
				value={shownTime}
				class="seek seek-touch absolute inset-0 w-full cursor-pointer appearance-none bg-transparent"
				aria-label={i18n.t('player.seek')}
				aria-valuetext={`${clock(shownTime)} / ${clock(displayDuration)}`}
				oninput={onScrubInput}
				onchange={onScrubCommit}
				onpointerup={onScrubCommit}
				onpointercancel={onScrubCommit}
			/>
		</div>
	</div>
	{#if scrubbing}
		<!-- 拖动中在画面中间显示目标时间，手指不用盯着底部细条；拖进锁定段（试看边界之后）时显示锁标，提示到此为止 -->
		<div
			class="pointer-events-none absolute top-1/2 left-1/2 z-20 flex -translate-x-1/2 -translate-y-1/2 items-center gap-1.5 rounded-2xl bg-black/80 px-4 py-2 text-base font-semibold text-white tabular lg:hidden"
		>
			{#if scrubTime > playableMax}<Icon name="lock" size={16} class="text-white/80" />{/if}
			{clock(scrubTime)} <span class="text-white/60">/ {clock(displayDuration)}</span>
		</div>
	{/if}

	<!-- 电脑端控制条 -->
	<div
		class="absolute inset-x-0 bottom-0 z-10 hidden bg-gradient-to-t from-black/85 via-black/40 to-transparent px-4 pt-14 pb-3 text-white transition-opacity duration-200 lg:block {controlsVisible ||
		settingsOpen
			? 'opacity-100'
			: 'pointer-events-none opacity-0'}"
	>
		<div class="group/seek relative mb-2 h-4">
			<div class="absolute inset-x-0 top-1/2 h-1 -translate-y-1/2 rounded-full bg-white/20 transition-[height] group-hover/seek:h-1.5">
				<div class="absolute inset-y-0 start-0 rounded-full bg-white/30" style:width="{bufferedPct}%"></div>
				<div class="absolute inset-y-0 start-0 rounded-full bg-brand" style:width="{progress}%"></div>
				{#if lockedPct < 100}
					<div class="locked-zone absolute inset-y-0 end-0 rounded-e-full" style:width="{100 - lockedPct}%" aria-hidden="true"></div>
				{/if}
			</div>
			<input
				type="range"
				min="0"
				max={displayDuration || 0}
				step="0.1"
				value={shownTime}
				class="seek absolute inset-0 w-full cursor-pointer appearance-none bg-transparent"
				aria-label={i18n.t('player.seek')}
				aria-valuetext={`${clock(shownTime)} / ${clock(displayDuration)}`}
				oninput={onScrubInput}
				onchange={onScrubCommit}
				onpointerup={onScrubCommit}
				onpointercancel={onScrubCommit}
			/>
		</div>
		<!-- 竖屏视频在矮窗口里只有二三百像素宽，控制条必须在任何宽度都不换行：
		     每一项都 shrink-0（见 <style> 里的 .ctl），时间用 nowrap，否则时间会在 span 内部折成两行 -->
		<div class="flex items-center {narrow ? 'gap-0.5' : 'gap-1'}">
			<button type="button" class="ctl" aria-label={i18n.t(player.phase === 'playing' ? 'player.pause' : 'player.play')} onclick={() => player.toggle()}>
				<Icon name={player.phase === 'playing' ? 'pause' : 'play'} size={20} filled strokeWidth={0} />
			</button>
			<!-- 下一集：非刷剧总显示（无下一集时禁用）；刷剧流只在同剧还有下一集时显示，否则上下滑是换剧。
			     图标保留描边：skip-forward 的竖线是一条 path，strokeWidth 为 0 时会消失，剩下的三角形和播放键一模一样 -->
			{#if !feed || hasNext}
				<button type="button" class="ctl" aria-label={i18n.t('player.next')} disabled={!hasNext} onclick={onnext}>
					<Icon name="skip-forward" size={19} filled strokeWidth={1.75} />
				</button>
			{/if}
			<span class="ms-1 shrink-0 text-[11px] whitespace-nowrap text-white/85 tabular xl:text-xs">
				{clock(shownTime)} / {clock(displayDuration)}
			</span>
			<span class="min-w-1 flex-1"></span>
			<button
				type="button"
				class="ctl"
				aria-label={i18n.t(player.muted ? 'player.unmute' : 'player.mute')}
				onclick={() => player.setMuted(!player.muted)}
			>
				<Icon name={player.muted || player.volume === 0 ? 'volume-x' : 'volume-2'} size={19} />
			</button>
			{#if !narrow}
				<input
					type="range"
					min="0"
					max="1"
					step="0.05"
					value={player.muted ? 0 : player.volume}
					class="volume w-16 shrink-0 cursor-pointer accent-white"
					aria-label={i18n.t('player.volume')}
					oninput={(event) => player.setVolume(Number(event.currentTarget.value))}
				/>
			{/if}
			<div class="relative">
				<button
					type="button"
					class="ctl"
					aria-label={i18n.t('player.settings')}
					aria-expanded={settingsOpen}
					aria-haspopup="dialog"
					onclick={() => (settingsOpen = !settingsOpen)}
				>
					<Icon name="settings" size={19} class={settingsOpen ? 'rotate-45 transition-transform' : 'transition-transform'} />
				</button>
			</div>
			<button
				type="button"
				class="ctl"
				aria-label={i18n.t(fullscreen ? 'player.exitFullscreen' : 'player.fullscreen')}
				onclick={toggleFullscreen}
			>
				<Icon name={fullscreen ? 'minimize' : 'maximize'} size={18} />
			</button>
		</div>
	</div>

	{#if settingsOpen}
		<div
			class="absolute end-3 bottom-[4.75rem] z-30 w-[min(18rem,calc(100%-1.5rem))] animate-rise rounded-2xl border border-white/10 bg-black/80 p-3 text-sm text-white shadow-pop backdrop-blur-xl"
			role="dialog"
			aria-label={i18n.t('player.settings')}
		>
			<p class="mb-1.5 px-1 text-xs text-white/60">{i18n.t('player.speed')}</p>
			<div class="mb-3 grid grid-cols-5 gap-1">
				{#each SPEEDS as speed (speed)}
					<button
						type="button"
						class="h-8 rounded-lg text-xs font-semibold tabular {player.rate === speed ? 'bg-brand' : 'bg-white/10 hover:bg-white/20'}"
						aria-pressed={player.rate === speed}
						onclick={() => player.setRate(speed)}>{speed}x</button
					>
				{/each}
			</div>
			{#if player.qualities.length > 1}
				<p class="mb-1.5 px-1 text-xs text-white/60">{i18n.t('player.quality')}</p>
				<div class="mb-3 flex flex-wrap gap-1">
					<button
						type="button"
						class="h-8 rounded-lg px-3 text-xs font-semibold {player.quality === -1 ? 'bg-brand' : 'bg-white/10 hover:bg-white/20'}"
						aria-pressed={player.quality === -1}
						onclick={() => player.setQuality(-1)}
					>
						{i18n.t('player.auto')}{player.quality === -1 && player.activeQualityLabel ? ` · ${player.activeQualityLabel}` : ''}
					</button>
					{#each player.qualities as option (option.index)}
						<button
							type="button"
							class="h-8 rounded-lg px-3 text-xs font-semibold {player.quality === option.index ? 'bg-brand' : 'bg-white/10 hover:bg-white/20'}"
							aria-pressed={player.quality === option.index}
							onclick={() => player.setQuality(option.index)}>{option.label}</button
						>
					{/each}
				</div>
			{/if}
			{#if player.lines.length > 0}
				<p class="mb-1.5 px-1 text-xs text-white/60">{i18n.t('player.line')}</p>
				<ul class="mb-2 space-y-1">
					{#each player.lines as line, index (line.line_id)}
						<li>
							<button
								type="button"
								class="flex h-9 w-full items-center justify-between rounded-lg px-3 text-start {player.lineIndex === index
									? 'bg-white/15'
									: 'hover:bg-white/10'}"
								aria-pressed={player.lineIndex === index}
								onclick={() => player.setLine(index)}
							>
								<span class="truncate">{lineLabel(index)}</span>
								<span class="ms-2 shrink-0 text-[11px] text-white/60">
									{#if player.lineIndex === index}{i18n.t('player.linePlaying')}{:else if line.manual}{i18n.t('player.lineManual')}{:else if line.recommended}{i18n.t('player.lineRecommended')}{:else}{line.region_tag}{/if}
								</span>
							</button>
						</li>
					{/each}
				</ul>
			{/if}
			<div class="flex gap-2">
				<button type="button" class="h-9 flex-1 rounded-lg bg-white/10 text-xs font-semibold hover:bg-white/20" onclick={() => player.retry()}>
					{i18n.t('player.refreshLine')}
				</button>
				<button type="button" class="h-9 flex-1 rounded-lg bg-white/10 text-xs font-semibold hover:bg-white/20" onclick={() => player.seek(0)}>
					{i18n.t('player.restart')}
				</button>
			</div>
			<p class="mt-2 px-1 text-[11px] leading-4 text-white/55">{i18n.t('player.switchHint')}</p>
		</div>
	{/if}

	<span class="sr-only" aria-live="polite">{title} {episodeLabel}</span>
</div>

<style>
	.ctl {
		display: inline-grid;
		/* 控制条在窄播放框里不允许被压缩，否则图标互相重叠、时间被挤到换行 */
		flex-shrink: 0;
		place-items: center;
		/* 固定 34px：别按视口断点放大，窄播放框（竖屏视频 ~295px）里会把控制条挤到换行 */
		width: 2.125rem;
		height: 2.125rem;
		border-radius: 9999px;
		transition: background-color 150ms;
	}
	.ctl:hover {
		background: rgb(255 255 255 / 0.14);
	}
	.ctl:disabled {
		opacity: 0.35;
		pointer-events: none;
	}
	.seek::-webkit-slider-thumb {
		appearance: none;
		width: 14px;
		height: 14px;
		border-radius: 9999px;
		background: #fff;
		box-shadow: 0 0 0 4px rgb(225 29 72 / 0.35);
		opacity: 0;
		transition: opacity 150ms;
	}
	.seek:hover::-webkit-slider-thumb,
	.seek:focus-visible::-webkit-slider-thumb {
		opacity: 1;
	}
	.seek::-moz-range-thumb {
		width: 14px;
		height: 14px;
		border: 0;
		border-radius: 9999px;
		background: #fff;
	}
	.seek::-webkit-slider-runnable-track,
	.seek::-moz-range-track {
		background: transparent;
	}
	/* 试看锁定段：进度条上试看边界之后的一段用斜纹画出，表示「此段需登录 / 开通会员解锁」，与可播区间区分 */
	.locked-zone {
		background-image: repeating-linear-gradient(
			-45deg,
			rgb(255 255 255 / 0.3) 0,
			rgb(255 255 255 / 0.3) 2px,
			transparent 2px,
			transparent 5px
		);
	}
	/* 触屏进度条：横向拖动自己处理，不交给浏览器（否则会被刷剧流的竖向 scroll-snap 抢走手势）。
	   滑块常驻可见、比电脑端大一圈，方便手指按住。 */
	.seek-touch {
		touch-action: none;
	}
	.seek-touch::-webkit-slider-thumb {
		width: 16px;
		height: 16px;
		opacity: 1;
		box-shadow: 0 0 0 5px rgb(0 0 0 / 0.25);
	}
	.seek-touch::-moz-range-thumb {
		width: 16px;
		height: 16px;
	}
</style>
