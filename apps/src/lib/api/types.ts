/**
 * 用户端接口的数据类型（与 service/api/openapi.yaml 一致，字段名保持 snake_case）。
 * 标注「预留」的类型对应 docs/07 已列出、后端尚未实现的接口：前台按契约先接好，接口返回 404 时按空数据处理。
 */

export type DramaCard = {
	id: number;
	title: string;
	slug: string;
	/** 竖图（相对 key 或 http(s) 完整地址，见 #lib/media/image.ts） */
	cover_portrait: string;
	cover_landscape: string;
	badge: string;
	score: number | null;
	year: number | null;
	is_finished: boolean;
	/** 可播放的最大集号；0 表示还没有可播放的分集 */
	latest_episode: number;
	play_count: number;
	publish_at: string | null;
};

export type DramaTag = { id: number; name: string; slug: string; is_sensitive: boolean };

export type DramaTerm = {
	id: number;
	name: string;
	slug: string;
	code: string;
	is_channel: boolean;
	taxonomy_id: number;
	taxonomy_name: string;
	taxonomy_slug: string;
};

export type EpisodeAccess = 'free' | 'login' | 'vip';

export type DramaEpisode = {
	id: number;
	number: number;
	title: string;
	cover: string;
	duration_sec: number;
	access: EpisodeAccess;
	trial_sec: number;
	playable: boolean;
};

export type DramaDetail = DramaCard & {
	aliases: string[];
	summary: string;
	episode_count: number;
	seo_title: string;
	seo_desc: string;
	tags: DramaTag[];
	terms: DramaTerm[];
	episodes: DramaEpisode[];
	related: DramaCard[];
};

export type CursorPage<T> = { list: T[]; next_cursor: string | null; total: number };

export type Channel = {
	id: number;
	name: string;
	slug: string;
	code: string;
	taxonomy_id: number;
	taxonomy_name: string;
	taxonomy_slug: string;
};

export type ChannelPage = { channel: Channel; list: DramaCard[]; total: number; page: number; page_size: number };

export type TagInfo = { id: number; name: string; slug: string; canonical_slug: string; is_sensitive: boolean };
export type TagPage = CursorPage<DramaCard> & { tag: TagInfo };

export type FacetTerm = { id: number; name: string; slug: string; code: string; is_channel: boolean };
export type FacetTaxonomy = { id: number; name: string; slug: string; multi_select: boolean; terms: FacetTerm[] };
export type ExplorePage = CursorPage<DramaCard> & { facets?: { taxonomies: FacetTaxonomy[] } };

export type TopicCard = { id: number; title: string; slug: string; summary: string };
export type TopicDetail = TopicCard & {
	source_type: 'manual' | 'auto';
	faq: { question: string; answer: string }[];
	dramas: DramaCard[];
};

export type SearchPage = CursorPage<DramaCard> & { query: string };
export type SuggestPage = { query: string; list: { id: number; title: string }[] };
export type HotWord = { word: string; pinned: boolean };

export type RankBoardCode = 'drama_week' | 'drama_month';
export type RankBoard = {
	board: RankBoardCode;
	computed_at: string | null;
	list: { rank: number; heat: number; drama: DramaCard }[];
};

export type BlockType =
	| 'carousel'
	| 'feature'
	| 'poster_grid'
	| 'h_scroll'
	| 'rank'
	| 'topic_group'
	| 'tag_cloud'
	| 'ad'
	| 'announce'
	| 'custom_html';

export type Device = 'all' | 'mobile' | 'desktop';

export type HomeBlock = {
	id: number;
	type: BlockType;
	title: string;
	subtitle: string;
	more_link: string;
	cols_desktop: number;
	device: Device;
	dramas?: DramaCard[];
	rank_board?: string;
	topics?: TopicCard[];
	tags?: { id: number; name: string; slug: string }[];
	ad_slot?: string;
	announce_kind?: string;
	html?: string;
};

export type PublicHome = { page: string; blocks: HomeBlock[] };

export type MenuPosition = 'top' | 'mobile_tabbar' | 'mobile_drawer' | 'footer';
export type PublicMenu = {
	id: number;
	position: MenuPosition;
	label: string;
	link_type: 'internal' | 'page' | 'channel' | 'topic' | 'tag' | 'external';
	link_value: string;
	group_title: string;
	icon: string;
	tabbar_icon: string;
	badge: string;
	visible_to: 'all' | 'auth' | 'guest';
	device: Device;
	new_tab: boolean;
};

export type SiteAddresses = {
	notice: string;
	latest_url: string;
	permanent_url: string;
	backup_urls: string[];
	publish_page: string;
	support_url: string;
	telegram: string;
	socials: { name: string; url: string }[];
};

export type AgeGate = {
	enabled?: boolean;
	title?: string;
	desc?: string;
	confirm_text?: string;
	deny_text?: string;
	deny_url?: string;
	remember_days?: number;
};

export type AnnouncementPageKey =
	| 'home'
	| 'explore'
	| 'channel'
	| 'tag'
	| 'topic'
	| 'drama'
	| 'play'
	| 'shua'
	| 'rank'
	| 'search'
	| 'me';

export type Announcement = {
	id: number;
	kind: 'topbar' | 'popup';
	content: string;
	click_url: string;
	frequency: 'always' | 'once' | 'daily';
	pages: AnnouncementPageKey[];
	image: string;
};

export type AdSlot =
	| 'global_popup'
	| 'home_top'
	| 'home_mid'
	| 'home_bottom'
	| 'footer_banner'
	| 'list_feed'
	| 'detail_banner'
	| 'preroll'
	| 'pause'
	| 'immersive_feed'
	| 'text_link'
	| 'app_download';

export type PublicAd = {
	id: number;
	slot: AdSlot;
	creative_type: 'image' | 'video' | 'text' | 'html';
	image: string;
	video_url: string;
	text: string;
	html: string;
	alt: string;
	click_url: string;
};

export type StaticPage = {
	slug: string;
	title: string;
	body: string;
	seo_title: string;
	seo_desc: string;
	updated_at: string;
};

export type PlaybackLine = {
	line_id: number;
	name: string;
	region_tag: string;
	recommended: boolean;
	manual: boolean;
	/** 同源加密清单地址 /api/v1/hls/{令牌}/index.m3u8，令牌不透明 */
	url: string;
	expires_at: string;
};

export type PlaybackSources = {
	episode_id: number;
	region: string;
	access: EpisodeAccess;
	/** 0 = 完整播放；大于 0 时清单只含这么多秒 */
	trial_sec: number;
	lines: PlaybackLine[];
};

export type MeState = { user: { id: number; nickname: string; avatar: string }; unread: number };

export type MeProfile = {
	nickname: string;
	avatar: string;
	bio: string;
	nickname_status: 'ok' | 'pending' | 'rejected';
	bio_status: 'ok' | 'pending' | 'rejected';
	avatar_status: 'ok' | 'pending' | 'rejected';
	locale?: string;
};

/** GET /api/v1/me/follows 的一项：剧卡片字段 + 这个用户的观看进度 */
export type FollowItem = DramaCard & {
	/** 看到第几集；0 表示还没看过 */
	watched_episode: number;
	followed_at: string;
};

export type HistoryItem = {
	drama_id: number;
	title: string;
	cover: string;
	episode_id: number;
	episode_number: number;
	position_sec: number;
	duration_sec: number;
	latest_episode: number;
	updated_at: string;
};

export type HistoryEntry = {
	drama_id: number;
	episode_id: number;
	episode_number: number;
	position_sec: number;
	duration_sec: number;
};

export type MeSession = {
	id: string;
	platform: 'web' | 'ios' | 'android';
	ua_digest?: string;
	ip_prefix?: string;
	current: boolean;
	created_at: string;
	last_seen_at: string;
};

export type Notification = {
	id: number;
	type: 'reply' | 'like' | 'update';
	payload: Record<string, unknown>;
	is_read: boolean;
	/** unix 秒 */
	created_at: number;
};

export type CommentView = {
	id: number;
	parent_id?: number;
	content: string;
	image_count: number;
	like_count: number;
	created_at: string;
	author: { is_guest: boolean; user_id?: number; nickname: string; avatar?: string };
};

export type CommentPage = { list: CommentView[]; next_cursor?: string };

/** POST /api/v1/episodes/{id}/comments 的回执：status=pending 表示要等审核通过才对其他人可见。 */
export type CommentReceipt = {
	id: number;
	episode_id: number;
	parent_id?: number;
	content: string;
	status: 'visible' | 'pending';
	like_count: number;
	created_at: string;
};

/** GET /api/v1/emojis：表情包与其表情（image 是图片 key 或完整地址）。 */
export type EmojiPack = { id: number; name: string; emojis: { code: string; image: string }[] };

export type AuthChallenge = {
	nonce: string;
	difficulty: number;
	expires_in: number;
	algorithm: 'sha256-leading-zero-bits';
};

/** GET /api/v1/vip/plans 的套餐。名称和角标由用户端多语言文案按 code / badge_key 映射。 */
export type VipPlan = {
	id: number;
	code: 'day' | 'week' | 'month' | 'quarter' | 'year';
	duration_days: number;
	price_fen: number;
	list_price_fen: number;
	price_micro_usdt: number;
	badge_key: string;
	recommended: boolean;
	channels: Array<'alipay' | 'wechat' | 'usdt' | 'iap_apple' | 'iap_google'>;
};

export type VipPlansResponse = { client: 'web_pc' | 'web_mobile' | 'app_official' | 'app_store'; plans: VipPlan[]; mock_enabled: boolean };

/** GET /api/v1/me/vip：个人数据，只能在浏览器登录态确认后请求。 */
export type VipStatus = { is_vip: boolean; expire_at: string | null };
