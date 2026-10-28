/**
 * 关页时没发出去的埋点暂存到本机，下次打开时补发（W4-1，docs/04 C.10）。
 *
 * 浏览器调 /api/v1/* 必须报文加密，而加密是异步的（WebCrypto）：页面被卸载（电脑上直接关标签页、
 * 整页跳走）时来不及做完，那一刻还没发出去的事件就丢了——sendBeacon 带不了 X-Enc 头，替代不了。
 * 所以页面一隐藏，就把还没确认送达的事件同步写进 localStorage，送达后删掉；页面真被卸载了，
 * 下一次（同一台设备 10 分钟内）打开本站时补发。超过 10 分钟的丢掉：服务端按收到的时间记事件，
 * 隔太久再补会把数据记到错误的时段，甚至错误的一天。
 *
 * 每个页面（文档）写自己的键，几个标签页同时开着也互不覆盖。只存事件本身（剧 / 集编号、路由、广告编号等），
 * 不含身份信息。
 */
import type { TrackEvent } from './queue.ts';

export type OutboxStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem' | 'key' | 'length'>;

export const OUTBOX_PREFIX = 'xs_evq:';
/** 暂存多久之内还补发 */
export const OUTBOX_TTL_MS = 10 * 60_000;
const MAX_EVENTS = 300;

/** 写入本页面的暂存；没有待发事件时删除 */
export function saveOutbox(storage: OutboxStorage, key: string, events: TrackEvent[], now: number) {
	try {
		if (events.length === 0) {
			storage.removeItem(key);
		} else {
			storage.setItem(key, JSON.stringify({ t: now, events: events.slice(-MAX_EVENTS) }));
		}
	} catch {
		// 存储满了或被禁用：这一批只能靠正常发送
	}
}

/** 取走其他页面留下的、10 分钟之内的暂存（取走即删除）；过期与损坏的一并清掉 */
export function takeOutboxes(storage: OutboxStorage, ownKey: string, now: number): TrackEvent[] {
	const keys: string[] = [];
	try {
		for (let i = 0; i < storage.length; i += 1) {
			const key = storage.key(i);
			if (key?.startsWith(OUTBOX_PREFIX) && key !== ownKey) {
				keys.push(key);
			}
		}
	} catch {
		return [];
	}
	const events: TrackEvent[] = [];
	for (const key of keys) {
		try {
			const saved = JSON.parse(storage.getItem(key) ?? 'null') as { t?: unknown; events?: unknown } | null;
			const age = typeof saved?.t === 'number' ? now - saved.t : -1;
			if (age >= 0 && age <= OUTBOX_TTL_MS && Array.isArray(saved?.events)) {
				for (const event of saved.events as unknown[]) {
					if (event && typeof event === 'object' && typeof (event as { type?: unknown }).type === 'string') {
						events.push(event as TrackEvent);
					}
				}
			}
		} catch {
			// 内容损坏：下面直接删掉
		}
		try {
			storage.removeItem(key);
		} catch {
			// 删不掉也无妨：过期之后不会再补发
		}
	}
	return events.slice(-MAX_EVENTS);
}
