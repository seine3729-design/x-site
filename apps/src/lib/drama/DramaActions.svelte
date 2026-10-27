<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import type { DramaCard } from '#lib/api/types.ts';
	import { compactNumber } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { splitLocale } from '#lib/i18n/locales.ts';
	import { localFollows, localLikes } from '#lib/local/store.ts';
	import SharePanel from '#lib/share/SharePanel.svelte';
	import { featureOn, getSite } from '#lib/site.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import { toasts } from '#lib/stores/toast.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import { setFollow, setLike } from './interactions.ts';
	import { dramaPath } from './labels.ts';

	type Props = {
		drama: DramaCard;
		/** row：详情页一排胶囊按钮；rail：播放页 / 刷剧手机右侧竖排图标 */
		variant?: 'row' | 'rail';
		/** 传了就在 rail 里「赞」后面加评论入口（播放页与刷剧用，详情页没有评论区） */
		oncomment?: () => void;
		/** 已加载的评论条数，0 时按钮显示「评论」 */
		commentCount?: number;
		/** 分享的目标地址（播放页 / 刷剧传当前集的地址），默认剧详情页 */
		shareUrl?: string;
		/** 海报上的副标题，如「第 3 集」 */
		shareSubtitle?: string;
	};

	let { drama, variant = 'row', oncomment, commentCount = 0, shareUrl, shareSubtitle }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());
	let followed = $state(false);
	let liked = $state(false);
	let busy = $state(false);

	onMount(() => {
		void localFollows.has(drama.id).then((v) => (followed = v));
		void localLikes.has('drama', drama.id).then((v) => (liked = v));
	});

	/** 游客互动关闭时，游客点追剧、点赞先去登录 */
	function needLogin(): boolean {
		if (session.loggedIn || featureOn(site.features, 'guest_interaction')) {
			return false;
		}
		const next = splitLocale(page.url.pathname).path + page.url.search;
		void goto(i18n.path(`/login?next=${encodeURIComponent(next)}`));
		return true;
	}

	async function toggleFollow() {
		if (busy || needLogin()) {
			return;
		}
		busy = true;
		const next = !followed;
		followed = next;
		try {
			await setFollow(drama, next);
			toasts.show(i18n.t(next ? 'drama.followDone' : 'drama.unfollowDone'), { tone: 'success' });
		} catch (err) {
			followed = !next;
			toasts.show(i18n.error(errorCode(err)), { tone: 'error' });
		} finally {
			busy = false;
		}
	}

	async function toggleLike() {
		if (needLogin()) {
			return;
		}
		const next = !liked;
		liked = next;
		try {
			await setLike('drama', drama.id, next);
		} catch (err) {
			liked = !next;
			toasts.show(i18n.error(errorCode(err)), { tone: 'error' });
		}
	}

	let shareOpen = $state(false);
	/** 分享地址：调用方没指定就用剧详情页（SSR 时没有 location，面板只在浏览器里打开，这里安全） */
	const target = $derived(shareUrl ?? new URL(i18n.path(dramaPath(drama.id)), page.url.origin).href);

	function errorCode(err: unknown): string {
		return err && typeof err === 'object' && 'code' in err ? String((err as { code: unknown }).code) : 'NETWORK';
	}
</script>

{#if variant === 'row'}
	<div class="flex flex-wrap items-center gap-2">
		<button
			type="button"
			class="btn btn-ghost {followed ? 'text-brand-fg' : ''}"
			aria-pressed={followed}
			onclick={toggleFollow}
		>
			<Icon name={followed ? 'bookmark-check' : 'bookmark'} size={18} filled={followed} />
			{i18n.t(followed ? 'drama.followed' : 'drama.follow')}
		</button>
		<button type="button" class="icon-btn size-11 bg-raised" aria-pressed={liked} aria-label={i18n.t(liked ? 'drama.liked' : 'drama.like')} onclick={toggleLike}>
			<Icon name="heart" size={19} filled={liked} class={liked ? 'text-brand-fg' : ''} />
		</button>
		<button type="button" class="icon-btn size-11 bg-raised" aria-label={i18n.t('share.title')} onclick={() => (shareOpen = true)}>
			<Icon name="share-2" size={18} />
		</button>
	</div>
{:else}
	<!-- 竖排按钮列压在视频上：用半透明实色而不是 backdrop-blur。
	     刷剧流里这几个按钮随列表滚动，backdrop-filter 每帧都要重新采样背景并模糊，是移动端滚动与点击卡顿的大头 -->
	<div class="flex flex-col items-center gap-5 text-white">
		<button type="button" class="flex flex-col items-center gap-1" aria-pressed={liked} onclick={toggleLike}>
			<span class="grid size-11 place-items-center rounded-full bg-black/45">
				<Icon name="heart" size={24} filled={liked} class={liked ? 'text-brand-fg' : ''} />
			</span>
			<span class="text-[11px] font-medium drop-shadow">{i18n.t(liked ? 'drama.liked' : 'drama.like')}</span>
		</button>
		{#if oncomment}
			<button type="button" class="flex flex-col items-center gap-1" onclick={oncomment}>
				<span class="grid size-11 place-items-center rounded-full bg-black/45">
					<Icon name="message-circle" size={23} />
				</span>
				<span class="text-[11px] font-medium drop-shadow tabular">
					{commentCount > 0 ? compactNumber(commentCount, i18n.locale) : i18n.t('comment.title')}
				</span>
			</button>
		{/if}
		<button type="button" class="flex flex-col items-center gap-1" aria-pressed={followed} onclick={toggleFollow}>
			<span class="grid size-11 place-items-center rounded-full bg-black/45">
				<Icon name={followed ? 'bookmark-check' : 'bookmark'} size={23} filled={followed} class={followed ? 'text-vip-fg' : ''} />
			</span>
			<span class="text-[11px] font-medium drop-shadow">{i18n.t(followed ? 'drama.followed' : 'drama.follow')}</span>
		</button>
		<button type="button" class="flex flex-col items-center gap-1" onclick={() => (shareOpen = true)}>
			<span class="grid size-11 place-items-center rounded-full bg-black/45">
				<Icon name="share-2" size={22} />
			</span>
			<span class="text-[11px] font-medium drop-shadow">{i18n.t('share.short')}</span>
		</button>
	</div>
{/if}

<SharePanel
	open={shareOpen}
	onclose={() => (shareOpen = false)}
	title={drama.title}
	subtitle={shareSubtitle}
	url={target}
	cover={drama.cover_portrait}
/>
