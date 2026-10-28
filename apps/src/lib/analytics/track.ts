/**
 * 埋点入口（W4-1，docs/04 C.10）：页面与组件经 runtime.ts 调用 track()，攒批后经加密客户端发 POST /api/v1/events。
 *
 * - 本模块随 runtime.ts 动态导入，不进首屏包；加密客户端（约 5 KB br）在第一次发送时才动态导入。
 * - 不用 sendBeacon：浏览器调 /api/v1/* 必须走报文加密，加密要带 X-Enc 请求头，sendBeacon 带不了自定义头；
 *   改用 fetch 的 keepalive（docs/03 第 8.1 节允许两种做法）。
 * - 加密是异步的，页面被卸载（直接关标签页、整页跳走）时不一定来得及做完：页面一隐藏就把还没发出去的事件
 *   同步暂存到本机，请求一交给浏览器（keepalive，页面关掉也会送达）就从暂存里拿掉；
 *   真没发出去的，下次打开时补发（outbox.ts）。实测整页跳走时加密做不完、关标签页时有时做得完，两种都不丢不重。
 *   例外是整页跳走后页面进了往返缓存（pagehide 的 persisted 为 true）：页面可能被「后退」恢复、
 *   恢复后会接着发，若同时又被下一个页面补发就会重复，所以这种情况不暂存，只靠恢复后发送（被浏览器清掉则丢失）。
 * - 后端始终回 204、超配额静默丢弃，这里不读响应体；断网与 5xx 由队列有限次重试。
 * - 服务端渲染时什么都不做。
 */
import { OUTBOX_PREFIX, saveOutbox, takeOutboxes, type OutboxStorage } from './outbox.ts';
import { createQueue, type EventQueue, type EventType, type TrackEvent } from './queue.ts';

export type { EventProps, EventType, TrackEvent } from './queue.ts';
export type TrackFields = Omit<TrackEvent, 'type'>;

const EVENTS_PATH = '/api/v1/events';

const inBrowser = typeof window !== 'undefined' && typeof document !== 'undefined';

/** 本页面（文档）的暂存键：每次打开页面都不同，几个标签页同时开着也互不覆盖 */
const ownKey = OUTBOX_PREFIX + Date.now().toString(36) + Math.random().toString(36).slice(2, 8);

async function send(events: TrackEvent[], dispatched: () => void): Promise<boolean> {
	// 设备标识必须带上：UV、剧集观看人数都按 X-Device-Id 去重
	const { deviceSecureFetch } = await import('#lib/api/client.ts');
	const response = await deviceSecureFetch(EVENTS_PATH, {
		method: 'POST',
		headers: { accept: 'application/json' },
		body: JSON.stringify({ client: 'web', events }),
		keepalive: true,
		// 请求交给浏览器的那一刻：这批不再算「没发出去」，从本机暂存里拿掉，否则关页后下次打开又补发一遍
		onSend: dispatched
	});
	// 4xx 是请求本身有问题，重发也一样，直接放弃
	return response.status < 500;
}

let queue: EventQueue | null = null;
/** 页面正进入往返缓存（见文件头）：这段时间不暂存 */
let inBackForwardCache = false;

function storage(): OutboxStorage | null {
	try {
		return window.localStorage;
	} catch {
		return null;
	}
}

/** 把还没确认送达的事件同步写进本机（只在页面隐藏期间需要） */
function persist() {
	const store = storage();
	if (store && queue && !inBackForwardCache) {
		saveOutbox(store, ownKey, queue.pending(), Date.now());
	}
}

/** 页面还活着且在前台：内存里的队列会接着发，暂存作废 */
function clearPersisted() {
	const store = storage();
	if (store) {
		saveOutbox(store, ownKey, [], Date.now());
	}
}

function ensureQueue(): EventQueue {
	if (!queue) {
		const created = createQueue({
			send,
			// 隐藏期间每批一交给浏览器、每批一有结果就更新暂存：发出去的拿掉，没发出去（含失败待重试）的留着
			onChange: () => {
				if (document.visibilityState === 'hidden') {
					persist();
				}
			}
		});
		queue = created;
		// 先同步写本机、再发：页面若正被卸载，发送做不完，暂存的这份下次打开时补发
		const onHidden = () => {
			persist();
			void created.flush();
		};
		document.addEventListener('visibilitychange', () => {
			if (document.visibilityState === 'hidden') {
				onHidden();
			} else {
				clearPersisted();
			}
		});
		// bfcache 与部分移动浏览器只发 pagehide，不一定先发 visibilitychange
		window.addEventListener('pagehide', (event) => {
			inBackForwardCache = event.persisted;
			if (event.persisted) {
				clearPersisted();
				void created.flush();
			} else {
				onHidden();
			}
		});
		window.addEventListener('pageshow', (event) => {
			inBackForwardCache = false;
			if (event.persisted) {
				clearPersisted();
			}
		});
		// 上一个页面被卸载时没发出去的，先补发
		const store = storage();
		if (store) {
			for (const event of takeOutboxes(store, ownKey, Date.now())) {
				created.push(event);
			}
		}
	}
	return queue;
}

/** 记一条事件。drama_id / episode_id 为 0 或缺省时不上报该字段 */
export function track(type: EventType, fields: TrackFields = {}) {
	if (!inBrowser) {
		return;
	}
	const event: TrackEvent = { type };
	if (fields.drama_id) {
		event.drama_id = fields.drama_id;
	}
	if (fields.episode_id) {
		event.episode_id = fields.episode_id;
	}
	if (fields.props) {
		event.props = fields.props;
	}
	const target = ensureQueue();
	target.push(event);
	// 页面已经隐藏时才入队的事件（播放器结算观看时长、Web Vitals 在隐藏时才定稿 CLS / INP）：
	// 关页时不会再有下一次定时发送，马上暂存并发出
	if (document.visibilityState === 'hidden') {
		persist();
		void target.flush();
	}
}

/** 立即发出已攒的事件（紧接着要跳转等场景） */
export function flushEvents() {
	if (inBrowser && queue) {
		void queue.flush();
	}
}
