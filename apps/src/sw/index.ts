/**
 * Service Worker（docs/04 D 节）：只为加载速度与断网兜底，不提供安装（站点没有 web app manifest）。
 *
 * - 带内容哈希的产物：缓存优先、永久有效；新版本激活时删除旧缓存。
 *   产物地址可能是同源 `/_app/immutable/*`，也可能是 CDN 绝对地址（`CDN_ASSET_PREFIX` 非空时），
 *   两种都由 `ASSET_URL_PREFIX` 统一判断，跨源响应按 `cors` 接受（仍然挡 `opaque`）。
 * - 页面导航：缓存 10 分钟内的直接返回并在后台刷新；更旧的先走网络（2.5 秒超时）再退回缓存；
 *   都没有时返回预缓存的离线页。服务端标了 no-store 的页面（个人页、错误页、写 Cookie 的响应）一律不缓存，
 *   缓存按完整地址（含查询串）区分，筛选与搜索不会串页。
 * - `/api/*` 一律走网络：报文加密响应每次密钥都不同，也含个人数据。
 *
 * **这个文件不由 SvelteKit 构建**，而是 `scripts/build-sw.ts` 在 `vite build` 之后用 Bun 编译成
 * `build/client/service-worker.js`。原因：SvelteKit 不允许自己管理的 Service Worker 与
 * `config.paths.assets` 共存（`service_worker_assets`），而前台的静态产物要走 CDN 绝对地址。
 * 构建版本与产物前缀由那个脚本以 `__SW_VERSION__` / `__SW_ASSET_BASE__` 注入。
 */
const sw = self as unknown as ServiceWorkerGlobalScope;

const version = __SW_VERSION__;
const PAGE_CACHE = `sv-pages-${version}`;
const STATIC_CACHE = `sv-static-${version}`;
const PRECACHE_PAGES = ['/', '/way-home', '/offline'];
const OFFLINE_PAGE = '/offline';
const FRESH_MS = 10 * 60_000;
const NETWORK_TIMEOUT_MS = 2500;
const MAX_PAGES = 40;
const CACHED_AT = 'sw-cached-at';

/** 产物前缀：`CDN_ASSET_PREFIX` 为空时是 `/_app/immutable/`，否则是 CDN 上的绝对地址。 */
const ASSET_BASE = __SW_ASSET_BASE__.replace(/\/$/, '');
const ASSET_PATH_PREFIX = `${ASSET_BASE}/_app/immutable/`;
const ASSET_URL_PREFIX = new URL(ASSET_PATH_PREFIX, sw.location.origin).href;
const CROSS_ORIGIN_ASSETS = !ASSET_URL_PREFIX.startsWith(`${sw.location.origin}/`);

function escapeRegExp(value: string): string {
	return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** 从预缓存页面的 HTML 里扒出产物地址。前缀跟着构建期注入的值走，跨源时能连域名一起匹配到。 */
const assetPattern = new RegExp(`${escapeRegExp(ASSET_PATH_PREFIX)}[^"'\\\\\\s)]+`, 'g');

function isAsset(url: URL): boolean {
	return url.href.startsWith(ASSET_URL_PREFIX);
}

function sameOrigin(url: URL) {
	return url.origin === sw.location.origin;
}

/** 页面缓存只收同源、可缓存的响应。 */
function cacheable(response: Response): boolean {
	const control = response.headers.get('cache-control') ?? '';
	return response.ok && response.type === 'basic' && !/no-store|private/.test(control);
}

/**
 * 产物缓存的收录条件。跨源取 CDN 上的产物时 `type` 是 `cors`，所以不能只认 `basic`；
 * 但一定要挡掉 `opaque`（CORS 没配好时就会退化成它：status 恒 0、存了读不出来还占配额）。
 */
function storableAsset(response: Response): boolean {
	return response.ok && (response.type === 'basic' || response.type === 'cors');
}

/** 存一份带缓存时间的副本，供判断新鲜度；超出上限时删最早的页面 */
async function rememberPage(request: Request | string, response: Response) {
	if (!cacheable(response)) {
		return;
	}
	const headers = new Headers(response.headers);
	headers.set(CACHED_AT, String(Date.now()));
	const body = await response.clone().arrayBuffer();
	const cache = await caches.open(PAGE_CACHE);
	await cache.put(request, new Response(body, { status: response.status, statusText: response.statusText, headers }));
	const keys = await cache.keys();
	const pages = keys.filter((key) => !PRECACHE_PAGES.includes(new URL(key.url).pathname));
	for (const stale of pages.slice(0, Math.max(0, pages.length - MAX_PAGES))) {
		await cache.delete(stale);
	}
}

async function precachePage(path: string) {
	const response = await fetch(path, { cache: 'no-store', credentials: 'same-origin', headers: { 'x-sv-precache': '1' } });
	if (!response.ok) {
		return;
	}
	const html = await response.clone().text();
	// 预缓存页面不看 Cache-Control（离线页本身是 no-store），直接存
	const headers = new Headers(response.headers);
	headers.set(CACHED_AT, String(Date.now()));
	await (await caches.open(PAGE_CACHE)).put(path, new Response(html, { status: 200, headers }));
	const staticCache = await caches.open(STATIC_CACHE);
	const assets = [...new Set(html.match(assetPattern) ?? [])];
	// 跨源产物不带自定义请求头：那会触发一次 CORS 预检，每个文件多一个往返
	const init: RequestInit = CROSS_ORIGIN_ASSETS ? {} : { headers: { 'x-sv-precache': '1' } };
	await Promise.all(
		assets.map(async (asset) => {
			if (await staticCache.match(asset)) {
				return;
			}
			const assetResponse = await fetch(asset, init);
			if (storableAsset(assetResponse)) {
				await staticCache.put(asset, assetResponse);
			}
		})
	);
}

async function cacheFirst(request: Request) {
	const cache = await caches.open(STATIC_CACHE);
	const cached = await cache.match(request);
	if (cached) {
		return cached;
	}
	const response = await fetch(request);
	if (storableAsset(response)) {
		await cache.put(request, response.clone());
	}
	return response;
}

async function networkFirst(request: Request) {
	const cache = await caches.open(PAGE_CACHE);
	try {
		const response = await fetch(request);
		if (cacheable(response)) {
			await cache.put(request, response.clone());
		}
		return response;
	} catch (error) {
		const cached = await cache.match(request);
		if (cached) {
			return cached;
		}
		throw error;
	}
}

function withTimeout<T>(promise: Promise<T>, ms: number): Promise<T> {
	return new Promise((resolve, reject) => {
		const timer = setTimeout(() => reject(new Error('timeout')), ms);
		promise.then(
			(value) => {
				clearTimeout(timer);
				resolve(value);
			},
			(error: unknown) => {
				clearTimeout(timer);
				reject(error);
			}
		);
	});
}

async function navigate(event: FetchEvent, request: Request): Promise<Response> {
	const cache = await caches.open(PAGE_CACHE);
	const cached = await cache.match(request);
	const network = fetch(request).then(async (response) => {
		await rememberPage(request, response);
		return response;
	});
	event.waitUntil(network.then(() => undefined).catch(() => undefined));

	const cachedAt = Number(cached?.headers.get(CACHED_AT));
	if (cached && Date.now() - cachedAt < FRESH_MS) {
		return cached;
	}
	try {
		return await withTimeout(network, cached ? NETWORK_TIMEOUT_MS : 15_000);
	} catch {
		if (cached) {
			return cached;
		}
		const offline = await cache.match(OFFLINE_PAGE);
		return (
			offline ??
			new Response('Offline', { status: 503, headers: { 'content-type': 'text/plain; charset=utf-8' } })
		);
	}
}

sw.addEventListener('install', (event) => {
	event.waitUntil(
		(async () => {
			await Promise.all(
				PRECACHE_PAGES.map((path) =>
					precachePage(path).catch(() => {
						// 预热失败不阻止安装，下一次访问仍走网络
					})
				)
			);
			await sw.skipWaiting();
		})()
	);
});

sw.addEventListener('activate', (event) => {
	event.waitUntil(
		(async () => {
			const keep = new Set([PAGE_CACHE, STATIC_CACHE]);
			const keys = await caches.keys();
			const stale = keys.filter((key) => key.startsWith('sv-') && !keep.has(key));
			await Promise.all(stale.map((key) => caches.delete(key)));
			await sw.clients.claim();
			if (stale.length === 0) {
				return;
			}
			const windows = await sw.clients.matchAll({ type: 'window', includeUncontrolled: true });
			for (const client of windows) {
				client.postMessage({ type: 'sv-reload' });
			}
		})()
	);
});

sw.addEventListener('fetch', (event) => {
	const request = event.request;
	if (request.method !== 'GET' || request.headers.get('x-sv-precache') === '1') {
		return;
	}
	const url = new URL(request.url);
	// 产物优先判断：CDN 绝对地址时它是跨源的，放在同源检查之后就永远走不到
	if (isAsset(url)) {
		event.respondWith(cacheFirst(request));
		return;
	}
	if (!sameOrigin(url) || url.pathname === '/service-worker.js' || url.pathname.startsWith('/api/')) {
		return;
	}
	if (url.pathname.endsWith('/__data.json') || url.pathname.endsWith('/__route.js')) {
		event.respondWith(networkFirst(request));
		return;
	}
	if (request.mode === 'navigate') {
		event.respondWith(navigate(event, request));
	}
});
