/**
 * SSR 读取公共数据（docs/04 A.3、docs/08 第 3 节）：BFF 经内网明文调用后端，进程内短缓存 + ETag 条件请求。
 *
 * 只放「同一个地址对所有人返回同样内容」的接口：结果会进入可被边缘缓存的页面，也在进程内跨请求共享。
 * 个人数据（me/*、选源、评论发表）一律由浏览器经加密接口二次请求，不经过这里。
 */
import { error } from '@sveltejs/kit';
import type {
	Announcement,
	ChannelPage,
	CursorPage,
	DramaDetail,
	ExplorePage,
	HotWord,
	PublicHome,
	RankBoard,
	RankBoardCode,
	SearchPage,
	StaticPage,
	TagPage,
	TopicCard,
	TopicDetail
} from '#lib/api/types.ts';
import type { Locale } from '#lib/i18n/locales.ts';
import { callService, ServiceError, type CallContext } from '#lib/server/api.ts';

type Entry = { data: unknown; etag: string | null; fetchedAt: number };

/** 进程内最多缓存这么多个地址（按最近使用淘汰），单条一般几 KB 到几十 KB。 */
const MAX_ENTRIES = 600;
const cache = new Map<string, Entry>();
const inflight = new Map<string, Promise<unknown>>();

function remember(path: string, entry: Entry) {
	cache.delete(path);
	cache.set(path, entry);
	if (cache.size > MAX_ENTRIES) {
		const oldest = cache.keys().next().value;
		if (oldest !== undefined) {
			cache.delete(oldest);
		}
	}
}

/**
 * 带缓存的 GET：ttl 内直接用进程内结果；过期后带 If-None-Match 回源（304 只刷新时间）。
 * 同一地址同一时刻只回源一次；后端暂时不可用（网络错误、5xx）时退回上一次的结果，页面照常渲染。
 */
export async function cachedGet<T>(ctx: CallContext, path: string, ttlMs: number): Promise<T> {
	const hit = cache.get(path);
	if (hit && Date.now() - hit.fetchedAt < ttlMs) {
		return hit.data as T;
	}
	let pending = inflight.get(path) as Promise<T> | undefined;
	if (!pending) {
		pending = revalidate<T>(ctx, path, hit).finally(() => inflight.delete(path));
		inflight.set(path, pending);
	}
	return pending;
}

async function revalidate<T>(ctx: CallContext, path: string, hit: Entry | undefined): Promise<T> {
	try {
		const { data, response } = await callService<T>(ctx, path, {
			headers: hit?.etag ? { 'if-none-match': hit.etag } : {},
			timeoutMs: 4_000
		});
		if (response.status === 304 && hit) {
			remember(path, { ...hit, fetchedAt: Date.now() });
			return hit.data as T;
		}
		remember(path, { data, etag: response.headers.get('etag'), fetchedAt: Date.now() });
		return data;
	} catch (err) {
		if (hit && (!(err instanceof ServiceError) || err.status >= 500)) {
			console.warn('回源失败，使用上一次的结果', path, err instanceof Error ? err.message : err);
			return hit.data as T;
		}
		throw err;
	}
}

/** 404 视为「没有这个接口或内容」时用：返回 fallback 而不是抛错（预留接口与可关闭的功能都走这里）。 */
export async function orFallback<T>(promise: Promise<T>, fallback: T): Promise<T> {
	try {
		return await promise;
	} catch (err) {
		if (err instanceof ServiceError && err.status < 500) {
			return fallback;
		}
		console.warn('读取公共数据失败，使用兜底内容', err instanceof Error ? err.message : err);
		return fallback;
	}
}

/** 把后端错误转换成页面错误：404 显示「内容不见了」，429 显示「操作太频繁」，其余按服务异常。 */
export function pageError(err: unknown): never {
	if (err instanceof ServiceError) {
		if (err.status === 404 || err.status === 400) {
			error(404, { message: 'NOT_FOUND', code: 'NOT_FOUND' });
		}
		if (err.status === 429) {
			error(429, { message: 'RATE_LIMITED', code: 'RATE_LIMITED' });
		}
		error(503, { message: err.code, code: err.code });
	}
	throw err;
}

const q = (params: Record<string, string | number | undefined | null>) => {
	const search = new URLSearchParams();
	for (const [key, value] of Object.entries(params).sort(([a], [b]) => a.localeCompare(b))) {
		if (value !== undefined && value !== null && value !== '') {
			search.set(key, String(value));
		}
	}
	const text = search.toString();
	return text ? `?${text}` : '';
};

/** 各页面用到的公共读接口。路径与查询参数见 docs/07「站点与内容读取」。 */
export const publicApi = {
	home: (ctx: CallContext, page = 'home') => cachedGet<PublicHome>(ctx, `/api/v1/home${q({ page })}`, 30_000),
	drama: (ctx: CallContext, id: number) => cachedGet<DramaDetail>(ctx, `/api/v1/dramas/${id}`, 30_000),
	channel: (ctx: CallContext, slug: string, params: { sort?: string; page?: number; page_size?: number }) =>
		cachedGet<ChannelPage>(ctx, `/api/v1/channels/${encodeURIComponent(slug)}${q(params)}`, 30_000),
	tag: (ctx: CallContext, slug: string, params: { sort?: string; cursor?: string; limit?: number }) =>
		cachedGet<TagPage>(ctx, `/api/v1/tags/${encodeURIComponent(slug)}${q(params)}`, 30_000),
	explore: (ctx: CallContext, params: Record<string, string | number | undefined>) =>
		cachedGet<ExplorePage>(ctx, `/api/v1/explore${q(params)}`, 30_000),
	topics: (ctx: CallContext, params: { cursor?: string; limit?: number }) =>
		cachedGet<CursorPage<TopicCard>>(ctx, `/api/v1/topics${q(params)}`, 60_000),
	topic: (ctx: CallContext, slug: string) =>
		cachedGet<TopicDetail>(ctx, `/api/v1/topics/${encodeURIComponent(slug)}`, 60_000),
	rank: (ctx: CallContext, board: RankBoardCode) => cachedGet<RankBoard>(ctx, `/api/v1/rank${q({ board })}`, 60_000),
	hotWords: (ctx: CallContext) =>
		cachedGet<{ list: HotWord[] }>(ctx, '/api/v1/search/hot', 60_000).then((d) => d.list),
	search: (ctx: CallContext, params: { q: string; cursor?: string; limit?: number; sort?: string }) =>
		cachedGet<SearchPage>(ctx, `/api/v1/search${q(params)}`, 20_000),
	announcements: (ctx: CallContext, locale: Locale) =>
		cachedGet<{ list: Announcement[] }>(ctx, `/api/v1/announcements${q({ locale })}`, 60_000).then((d) => d.list),
	page: (ctx: CallContext, slug: string) =>
		cachedGet<StaticPage>(ctx, `/api/v1/pages/${encodeURIComponent(slug)}`, 120_000)
};
