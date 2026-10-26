import { CDN_ASSET_PREFIX, IMAGE_BASE_URLS } from '$app/env/private';
import type { AgeGate, PublicMenu, SiteAddresses } from '#lib/api/types.ts';
import { callContext } from '#lib/server/context.ts';
import { CATALOGS } from '#lib/server/i18n-catalogs.ts';
import { orFallback, publicApi } from '#lib/server/public-api.ts';
import { getSiteConfig } from '#lib/server/site-config.ts';
import type { SiteData } from '#lib/site.ts';
import { normalizeLayout, normalizeTheme } from '#lib/theme/settings.ts';
import type { LayoutServerLoad } from './$types';

const text = (value: unknown) => (typeof value === 'string' ? value.trim() : '');

function addresses(value: unknown): SiteAddresses | null {
	if (!value || typeof value !== 'object') {
		return null;
	}
	const a = value as Partial<SiteAddresses>;
	const backups = Array.isArray(a.backup_urls) ? a.backup_urls.filter((u) => typeof u === 'string' && u) : [];
	const socials = Array.isArray(a.socials) ? a.socials.filter((s) => s && s.name && s.url) : [];
	const result: SiteAddresses = {
		notice: text(a.notice),
		latest_url: text(a.latest_url),
		permanent_url: text(a.permanent_url),
		backup_urls: backups,
		publish_page: text(a.publish_page),
		support_url: text(a.support_url),
		telegram: text(a.telegram),
		socials
	};
	const empty = !result.latest_url && !result.permanent_url && backups.length === 0 && !result.publish_page;
	return empty && !result.support_url && !result.telegram && socials.length === 0 ? null : result;
}

export const load: LayoutServerLoad = async (event) => {
	const ctx = callContext(event);
	const { locale } = event.locals;
	const messages = CATALOGS[locale];
	const [config, announcements] = await Promise.all([
		getSiteConfig(ctx),
		orFallback(publicApi.announcements(ctx, locale), [])
	]);
	const menus = (config as { menus?: PublicMenu[] }).menus ?? [];
	const footerPages = (config as { footer_pages?: { slug: string; title: string }[] }).footer_pages ?? [];

	// 只下发前台需要的公共配置与当前语言的文案；这里的数据会进入可被边缘缓存的页面，不能包含个人信息。
	// 配色本身不在这里下发：它在 SSR 时由 hooks.server.ts 写进 <html style>（docs/04 C.8）。
	const site: SiteData = {
		version: config.version,
		name: text(config.site.site_name) || messages['site.fallbackName'],
		slogan: text(config.site.slogan),
		logo: text(config.site.logo_light),
		favicon: text(config.site.favicon),
		copyright: text(config.site.copyright),
		features: config.features,
		ageGate: config.age_gate as AgeGate,
		theme: normalizeTheme(config.theme),
		layout: normalizeLayout(config.layout),
		menus,
		footerPages,
		addresses: addresses(config.addresses),
		imageBases: IMAGE_BASE_URLS,
		announcements
	};

	return {
		cdn: CDN_ASSET_PREFIX,
		i18n: { locale, messages },
		site
	};
};
