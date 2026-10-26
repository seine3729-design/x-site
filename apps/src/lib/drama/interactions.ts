/**
 * 追剧、点赞、分享（docs/04 B.5）：先改本机（游客数据 / 账号缓存）与界面，登录用户再同步到账号；
 * 失败时由调用方回滚界面状态。关闭「游客互动」时游客需先登录。
 */
import type { DramaCard } from '#lib/api/types.ts';
import { localFollows, localLikes } from '#lib/local/store.ts';
import { session } from '#lib/stores/session.svelte.ts';

export async function setFollow(card: DramaCard, on: boolean): Promise<void> {
	if (session.loggedIn) {
		const { api } = await import('#lib/api/client.ts');
		if (on) {
			await api.post('/api/v1/me/follow', { drama_id: card.id });
		} else {
			await api.del(`/api/v1/me/follow?drama_id=${card.id}`);
		}
	}
	if (on) {
		await localFollows.add({
			id: card.id,
			title: card.title,
			cover_portrait: card.cover_portrait,
			latest_episode: card.latest_episode,
			is_finished: card.is_finished,
			badge: card.badge,
			play_count: card.play_count,
			score: card.score,
			year: card.year,
			followed_at: Date.now()
		});
	} else {
		await localFollows.remove(card.id);
	}
}

export async function setLike(type: 'drama' | 'episode', id: number, on: boolean): Promise<void> {
	if (session.loggedIn) {
		const { api } = await import('#lib/api/client.ts');
		if (on) {
			await api.post('/api/v1/me/like', { target_type: type, target_id: id });
		} else {
			await api.del(`/api/v1/me/like?type=${type}&id=${id}`);
		}
	}
	if (on) {
		await localLikes.add(type, id);
	} else {
		await localLikes.remove(type, id);
	}
}

/** 系统分享（手机）或复制链接；返回 'shared' | 'copied' | 'failed' */
export async function shareLink(title: string, url: string): Promise<'shared' | 'copied' | 'failed'> {
	if (typeof navigator.share === 'function' && matchMedia('(pointer: coarse)').matches) {
		try {
			await navigator.share({ title, url });
			return 'shared';
		} catch (err) {
			if (err instanceof DOMException && err.name === 'AbortError') {
				return 'shared';
			}
		}
	}
	try {
		await navigator.clipboard.writeText(url);
		return 'copied';
	} catch {
		return 'failed';
	}
}
