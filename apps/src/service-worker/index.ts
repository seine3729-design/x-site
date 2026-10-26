/**
 * Service Worker（docs/04 D 节）：只为加载速度与断网兜底，不提供安装（站点没有 web app manifest）。
 *
 * - /_app/immutable/*：带内容哈希，缓存优先、永久有效；新版本激活时删除旧缓存。
 * - 页面导航：缓存 10 分钟内的直接返回并在后台刷新；更旧的先走网络（2.5 秒超时）再退回缓存；
 *   都没有时返回预缓存的离线页。服务端标了 no-store 的页面（个人页、错误页、写 Cookie 的响应）一律不缓存，
 *   缓存按完整地址（含查询串）区分，筛选与搜索不会串页。
 * - /api/* 一律走网络：报文加密响应每次密钥都不同，也含个人数据。
 */
import { version } from '$app/env';
import { self } from '$app/service-worker';

const PAGE_CACHE = `sv-pages-${version}`;
const STATIC_CACHE = `sv-static-${version}`;
const PRECACHE_PAGES = ['/', '/way-home', '/offline'];
const OFFLINE_PAGE = '/offline';
const FRESH_MS = 10 * 60_000;
const NETWORK_TIMEOUT_MS = 2500;
const MAX_PAGES = 40;
const CACHED_AT = 'sw-cached-at';
const assetPattern = /\/_app\/immutable\/[^"'\\\s)]+/g;

function sameOrigin(url: URL) {
	return url.origin === self.location.origin;
}

function cacheable(response: Response): boolean {
	const control = response.headers.get('cache-control') ?? '';
	return response.ok && response.type === 'basic' && !/no-store|private/.test(control);
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
	await Promise.all(
		assets.map(async (asset) => {
			if (await staticCache.match(asset)) {
				return;
			}
			const assetResponse = await fetch(asset, { headers: { 'x-sv-precache': '1' } });
			if (assetResponse.ok) {
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
	if (response.ok && response.type === 'basic') {
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

self.addEventListener('install', (event) => {
	event.waitUntil(
		(async () => {
			await Promise.all(
				PRECACHE_PAGES.map((path) =>
					precachePage(path).catch(() => {
						// 预热失败不阻止安装，下一次访问仍走网络
					})
				)
			);
			await self.skipWaiting();
		})()
	);
});

self.addEventListener('activate', (event) => {
	event.waitUntil(
		(async () => {
			const keep = new Set([PAGE_CACHE, STATIC_CACHE]);
			const keys = await caches.keys();
			const stale = keys.filter((key) => key.startsWith('sv-') && !keep.has(key));
			await Promise.all(stale.map((key) => caches.delete(key)));
			await self.clients.claim();
			if (stale.length === 0) {
				return;
			}
			const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
			for (const client of windows) {
				client.postMessage({ type: 'sv-reload' });
			}
		})()
	);
});

self.addEventListener('fetch', (event) => {
	const request = event.request;
	if (request.method !== 'GET' || request.headers.get('x-sv-precache') === '1') {
		return;
	}
	const url = new URL(request.url);
	if (!sameOrigin(url) || url.pathname === '/service-worker.js' || url.pathname.startsWith('/api/')) {
		return;
	}
	if (url.pathname.endsWith('/__data.json') || url.pathname.endsWith('/__route.js')) {
		event.respondWith(networkFirst(request));
		return;
	}
	if (url.pathname.startsWith('/_app/immutable/')) {
		event.respondWith(cacheFirst(request));
		return;
	}
	if (request.mode === 'navigate') {
		event.respondWith(navigate(event, request));
	}
});
