import type { PublicMenu } from '#lib/api/types.ts';
import { callService, type CallContext } from '#lib/server/api.ts';

/** 前台使用的站点配置（与 service/api/openapi.yaml 中 SiteConfig 一致）。 */
export type SiteConfig = {
	version: number;
	site: { site_name?: string; slogan?: string; [key: string]: unknown };
	theme: Record<string, unknown>;
	layout: Record<string, unknown>;
	seo: Record<string, unknown>;
	age_gate: { enabled?: boolean; [key: string]: unknown };
	features: Record<string, boolean>;
	addresses: Record<string, unknown>;
	menus?: PublicMenu[];
	footer_pages?: { slug: string; title: string }[];
	updated_at: string;
};

/** 后端不可用时的兜底配置，保证页面仍能渲染（站点名由文案 site.fallbackName 兜底）。 */
const FALLBACK: SiteConfig = {
	version: 0,
	site: {},
	theme: {},
	layout: {},
	seo: {},
	age_gate: { enabled: false },
	features: {},
	addresses: {},
	menus: [],
	footer_pages: [],
	updated_at: new Date(0).toISOString()
};

const REFRESH_MS = 30_000;

let cached: { config: SiteConfig; etag: string | null; fetchedAt: number } | null = null;
let inflight: Promise<SiteConfig> | null = null;

/**
 * 读取站点配置：进程内缓存 30 秒，过期后用 ETag 条件请求刷新；
 * 同一时刻只发一个刷新请求；后端失败时返回上一次成功的结果或兜底配置。
 */
export async function getSiteConfig(ctx: CallContext): Promise<SiteConfig> {
	if (cached && Date.now() - cached.fetchedAt < REFRESH_MS) {
		return cached.config;
	}
	inflight ??= refresh(ctx).finally(() => {
		inflight = null;
	});
	return inflight;
}

async function refresh(ctx: CallContext): Promise<SiteConfig> {
	try {
		const headers: HeadersInit = cached?.etag ? { 'if-none-match': cached.etag } : {};
		const { data, response } = await callService<SiteConfig>(ctx, '/api/v1/site-config', {
			headers,
			timeoutMs: 2_000
		});

		if (response.status === 304 && cached) {
			cached = { ...cached, fetchedAt: Date.now() };
			return cached.config;
		}

		cached = { config: data, etag: response.headers.get('etag'), fetchedAt: Date.now() };
		return data;
	} catch (error) {
		console.warn('读取站点配置失败，使用缓存或兜底配置', error);
		return cached?.config ?? FALLBACK;
	}
}
