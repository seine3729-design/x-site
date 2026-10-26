import type { DramaCard, DramaEpisode } from '#lib/api/types.ts';
import type { I18n } from '#lib/i18n/context.ts';

/** 「全 80 集」「更新至 12 集」「即将上线」 */
export function episodesLabel(i18n: I18n, card: Pick<DramaCard, 'is_finished' | 'latest_episode'>): string {
	if (card.latest_episode <= 0) {
		return i18n.t('drama.comingSoon');
	}
	return i18n.t(card.is_finished ? 'drama.episodesTotal' : 'drama.episodesUpdating', { n: card.latest_episode });
}

/** 剧详情 / 播放页地址（未加语言前缀） */
export const dramaPath = (id: number) => `/d/${id}`;
export const playPath = (id: number, episode: number) => `/play/${id}/${episode}`;

/** 第一集可播放的分集（「立即观看」的去向）；没有可播放的分集时为 null */
export function firstPlayable(episodes: DramaEpisode[]): DramaEpisode | null {
	return episodes.find((e) => e.playable) ?? null;
}
