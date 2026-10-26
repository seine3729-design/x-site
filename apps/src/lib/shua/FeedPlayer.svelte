<script lang="ts">
	import { onMount, untrack, type Snippet } from 'svelte';
	import type { DramaDetail } from '#lib/api/types.ts';
	import { dramaPath } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { localHistory } from '#lib/local/store.ts';
	import { PlayerController } from '#lib/player/controller.svelte.ts';
	import { reportLine } from '#lib/player/line-report.ts';
	import Player from '#lib/player/Player.svelte';

	type Props = {
		drama: DramaDetail;
		active: boolean;
		onended: () => void;
		/**
		 * 底部信息与右侧按钮列。必须由播放器渲染在它内部：
		 * Player 根节点带 isolation: isolate，自成层叠上下文，若这层 UI 画在播放器外面（页面层 z-10），
		 * 会整体盖在播放器之上，把播放器自己的进度条等控件的点击全部吃掉。
		 * 换集能力作为参数回传，页面才能把「看全集」和「下一集」排进同一行。
		 */
		info?: Snippet<[{ hasNext: boolean; onnext: () => void }]>;
	};
	let { drama, active, onended, info }: Props = $props();

	const i18n = getI18n();
	// 刷剧流挂着「上一条 / 当前 / 下一条」三个播放器，只有当前条在播：
	// 上下条先把选源、清单与首段缓冲好，滑过去时直接起播，不用再等一轮网络往返（docs/04 B.7）。
	// 预加载的两条缓冲上限压到 8 秒，成为当前条后由 activate() 调回正常值，长时间连刷内存也稳。
	// 一律按「预加载」建，再由下面的 $effect 决定谁是当前条（构造时读 active 只会拿到初值）
	const player = new PlayerController({ onLineReport: reportLine, autoplay: false, preloadBufferSec: 8 });
	// 刷剧流支持在同一部剧里往下看：维护当前集，提供「下一集」；播完一集自动连播，整部看完才由 onended 换下一条剧
	const playable = $derived(drama.episodes.filter((e) => e.playable));
	let episodeIndex = $state(0);
	const episode = $derived(playable[episodeIndex]);
	const hasNextEpisode = $derived(episodeIndex < playable.length - 1);

	function playNextEpisode() {
		if (episodeIndex < playable.length - 1) {
			episodeIndex += 1;
			void player.load(playable[episodeIndex]!.id);
		}
	}

	// 成为当前条就起播；滑走只暂停、保留缓冲，滑回来能立刻继续。
	// 只依赖 active：activate()/deactivate() 内部会读 player.phase 等 $state，若被本 effect 追踪，
	// 用户点暂停后 phase 变成 'paused' 会反过来触发 effect 重跑、又调 activate() 把画面重新播起来，
	// 表现就是「点暂停没反应」。用 untrack 隔离这些副作用里读到的状态。
	$effect(() => {
		const isActive = active;
		untrack(() => {
			if (isActive) {
				player.activate();
			} else {
				player.deactivate();
			}
		});
	});

	onMount(() => {
		if (episode) {
			void player.load(episode.id);
		}
		return () => {
			const position = Math.floor(player.currentTime);
			if (episode && position >= 2) {
				void localHistory.save({
					drama_id: drama.id,
					title: drama.title,
					cover: drama.cover_portrait,
					episode_id: episode.id,
					episode_number: episode.number,
					position_sec: position,
					duration_sec: Math.floor(player.duration) || episode.duration_sec,
					latest_episode: drama.latest_episode,
					updated_at: Date.now()
				});
			}
			player.destroy();
		};
	});

	$effect(() => {
		// 只有当前条播完才处理；预加载的那两条不会触发。还有下一集就连播，整部播完才翻到下一条剧
		if (active && player.phase === 'ended') {
			untrack(() => {
				if (hasNextEpisode) {
					playNextEpisode();
				} else {
					onended();
				}
			});
		}
	});
</script>

{#snippet playerOverlay()}
	{@render info?.({ hasNext: hasNextEpisode, onnext: playNextEpisode })}
{/snippet}

{#if episode}
	<Player
		{player}
		feed
		title={drama.title}
		episodeLabel={i18n.t('episode.label', { n: episode.number })}
		detailHref={i18n.path(dramaPath(drama.id))}
		hasNext={hasNextEpisode}
		hasPrev={false}
		onnext={playNextEpisode}
		onprev={() => {}}
		overlay={info ? playerOverlay : undefined}
	/>
{:else}
	<!-- 还没有可播放的分集：留着封面，标出「即将上线」 -->
	<div class="absolute inset-0 grid place-items-center">
		<div class="w-3/5 max-w-64 overflow-hidden rounded-2xl shadow-pop">
			<Poster cover={drama.cover_portrait} title={drama.title} />
		</div>
		<span class="absolute top-[18%] rounded-full bg-black/60 px-3 py-1 text-sm text-white backdrop-blur">{i18n.t('drama.comingSoon')}</span>
	</div>
{/if}
