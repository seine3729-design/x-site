/**
 * 来访与页面浏览埋点（W4-1，docs/04 C.10）：visitor 每个标签页会话一次，page_view 每次路由切换一次（含首屏）。
 * 只记路由模板、路径、来源域名与渠道参数；不记查询串（搜索词等用户输入在里面）、不记完整来源地址。
 * 根 layout 经 runtime.ts 动态导入本模块，埋点代码不进首屏包。
 */
import { splitLocale } from '#lib/i18n/locales.ts';
import { deviceIsNew } from '#lib/local/device.ts';
import { track } from './track.ts';
import { startWebVitals } from './vitals.ts';

/** 本标签页会话已经记过 visitor */
const SESSION_KEY = 'xs_vs';
const MAX_PATH = 200;

/** 渠道：`utm_source`，没有时取 `ch`；只收短的字母数字，其它一律不记（防止把任意输入写进埋点） */
export function channelOf(url: URL): string {
	const value = (url.searchParams.get('utm_source') ?? url.searchParams.get('ch') ?? '').trim();
	return /^[\w.-]{1,40}$/.test(value) ? value : '';
}

/** 来源只记域名，站内跳转与无来源记空 */
export function referrerHost(referrer: string, ownHost: string): string {
	if (!referrer) {
		return '';
	}
	try {
		const host = new URL(referrer).host;
		return host === ownHost ? '' : host.slice(0, 100);
	} catch {
		return '';
	}
}

/** 去掉语言前缀后的路径，不带查询串与锚点 */
export function pagePath(url: URL): string {
	return splitLocale(url.pathname).path.slice(0, MAX_PATH);
}

function firstInSession(): boolean {
	try {
		if (sessionStorage.getItem(SESSION_KEY)) {
			return false;
		}
		sessionStorage.setItem(SESSION_KEY, '1');
		return true;
	} catch {
		// 隐私模式写不进去：按每次打开记一次
		return true;
	}
}

/** 首屏（hydration 完成）时调用：本标签页第一次时记 visitor，然后记一次 page_view */
export function trackEntry(url: URL, routeId: string | null, locale: string) {
	if (firstInSession()) {
		track('visitor', {
			props: {
				new: deviceIsNew(),
				ch: channelOf(url) || undefined,
				ref: referrerHost(document.referrer, location.host) || undefined,
				entry: routeId ?? undefined,
				device: matchMedia('(min-width: 1024px)').matches ? 'desktop' : 'mobile',
				lang: locale
			}
		});
	}
	trackPageView(url, routeId);
}

export function trackPageView(url: URL, routeId: string | null) {
	track('page_view', { props: { route: routeId ?? undefined, path: pagePath(url) } });
}

/**
 * 根 layout 每次导航完成后调用（type 为 SvelteKit afterNavigate 的类型）：首屏记来访与浏览，
 * 并在浏览器空闲时开始测 Web Vitals（不和首屏抢主线程，docs/04 E；它读缓冲的性能条目，晚启动不丢数据）；
 * 之后每次路由切换记一次浏览。
 */
export function navigated(type: string, url: URL, routeId: string | null, locale: string) {
	if (type !== 'enter') {
		trackPageView(url, routeId);
		return;
	}
	trackEntry(url, routeId, locale);
	const start = () => void startWebVitals(routeId);
	if (typeof requestIdleCallback === 'function') {
		requestIdleCallback(start, { timeout: 5_000 });
	} else {
		setTimeout(start, 2_000);
	}
}
